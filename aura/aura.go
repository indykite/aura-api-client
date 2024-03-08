package aura

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	"github.com/hashicorp/go-retryablehttp"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/clientcredentials"
)

const endpoint = "https://api.neo4j.io"
const retries = 0
const version = "v1"

// AuraError is used to inject the request ID used by Neo4J support into
// error messages when possible and include the response body.
type AuraError struct {
	requestID string
	Err       error
	response  *http.Response
}

func (e *AuraError) Error() string {
	return fmt.Sprintf("Aura API error: %v\nAura request ID: %v\nResponse body: %v",
		e.Err, e.requestID, responseBodyToString(e.response))
}

// newAuraError returns an AuraError with the requestID set to the
// X-Request-Id header value of the given response. This requestID
// can be used by Neo4J staff to identify specific requests.
func newAuraError(err error, resp *http.Response) *AuraError {
	return &AuraError{
		requestID: resp.Header.Get("X-Request-Id"),
		Err:       err,
		response:  resp,
	}
}

// Client is the interface containing the methods for connecting to the Aura API.
type Client interface {
	CreateInstance(name, cloudProvider, memory, version, region, instanceType string) (*CreateResponse, error)
	GetInstance(id string) (*GetResponse, error)
	DestroyInstance(id string) error
	PauseInstance(id string) error
}

type client struct {
	httpClient *http.Client
	logger     *slog.Logger
	endpoint   string
	tenantID   string
	retries    int
	version    string
}

type option func(*client)

// NewClient creates a new client based on a given client ID and secret as well as
// options for customizing the returned client.
func NewClient(ctx context.Context, clientID, clientSecret, tenantID string, options ...option) (*client, error) {
	c := &client{
		logger:   slog.Default(),
		endpoint: endpoint,
		retries:  retries,
		tenantID: tenantID,
		version:  version,
	}
	for _, o := range options {
		o(c)
	}
	r := retryablehttp.NewClient()
	r.RetryMax = c.retries
	r.ErrorHandler = func(resp *http.Response, err error, numTries int) (*http.Response, error) {
		var m string
		if err != nil {
			m += fmt.Sprintln(err.Error())
		}
		if resp != nil {
			m += fmt.Sprintln(resp.Status)
		}
		e := errors.New(m + fmt.Sprintf(" Gave up after %d attempts", numTries))
		return resp, newAuraError(e, resp)
	}
	if c.httpClient == nil {
		c.httpClient = r.StandardClient()
	}
	conf := &clientcredentials.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		TokenURL:     c.endpoint + "/oauth/token",
	}
	ctx = context.WithValue(ctx, oauth2.HTTPClient, c.httpClient)
	c.httpClient = conf.Client(ctx)
	return c, nil
}

// WithHTTPClient sets the HTTP client used to communicate with Aura.
func WithHTTPClient(h *http.Client) option {
	return func(c *client) {
		c.httpClient = h
	}
}

// WithEndpoint sets the a custom endpoint for Aura.
func WithEndpoint(e string) option {
	return func(c *client) {
		c.endpoint = e
	}
}

// WithRetries sets the maximum number of retries for requests. By default
// we do not retry and the maximum number of retries are 3.
// Requests are retried with exp backoff on 5xx errors as recommended by Neo4J.
func WithRetries(n int) option {
	return func(c *client) {
		c.retries = n
	}
}

// WithLogger sets a custom logger instead instead of slog
func WithLogger(l *slog.Logger) option {
	return func(c *client) {
		c.logger = l
	}
}

// WithVersion sets the client to use a given API version
func WithVersion(v string) option {
	return func(c *client) {
		c.version = v
	}
}

// CreateResponse is returned when creating an Aura instance and is
// constructed from the values from
// https://neo4j.com/docs/aura/platform/api/specification/#/instances/post-instances.
type CreateResponse struct {
	ID            string // Internal ID of the instance
	ConnectionURL string // URL the instance is hosted at
	Username      string // Name of the initial admin user
	Password      string // Password of the initial admin user
	Name          string // The name we chose for the instance
	TenantID      string // Tenant for managing Aura console users
	CloudProvider string // GCP, AWS, ...
	Region        string // us-east1, eu-central2, ...
	InstanceType  string // enterprise-db, professional-db, ...
}

// newCreateResponse attempts to construct a CreateResponse struct from a given
// http response
func newCreateResponse(httpResp *http.Response) (*CreateResponse, error) {
	body, err := unmarshalResponse(httpResp)
	if err != nil {
		return nil, err
	}
	m, ok := body.(map[string]any)
	if !ok {
		return nil, errors.New(`expected response to be a map with string keys`)
	}
	resp := &CreateResponse{}
	if resp.ID, ok = m["id"].(string); !ok {
		return nil, errors.New(`response missing key "id" or value not string`)
	}
	if resp.ConnectionURL, ok = m["connection_url"].(string); !ok {
		return nil, errors.New(`response missing key "connection_url" or value not string`)
	}
	if resp.Username, ok = m["username"].(string); !ok {
		return nil, errors.New(`response missing key "username" or value not string`)
	}
	if resp.Password, ok = m["password"].(string); !ok {
		return nil, errors.New(`response missing key "password" or value not string`)
	}
	if resp.Name, ok = m["name"].(string); !ok {
		return nil, errors.New(`response missing key "name" or value not string`)
	}
	if resp.TenantID, ok = m["tenant_id"].(string); !ok {
		return nil, errors.New(`response missing key "tenant_id" or value not string`)
	}
	if resp.CloudProvider, ok = m["cloud_provider"].(string); !ok {
		return nil, errors.New(`response missing key "cloud_provider" or value not string`)
	}
	if resp.Region, ok = m["region"].(string); !ok {
		return nil, errors.New(`response missing key "region" or value not string`)
	}
	if resp.InstanceType, ok = m["type"].(string); !ok {
		return nil, errors.New(`response missing key "type" or value not string`)
	}
	return resp, nil
}

// CreateInstance attempts to create a new Aura instance with the given name
// returning information about the instance if succesful and otherwise
// returning an error.
// Possible values for the parameters can be found in the documentation of the Neo4J Aura API.
func (c *client) CreateInstance(name, cloudProvider, memory, version, region, instanceType string) (*CreateResponse, error) {
	req, err := c.newRequest("POST", c.api()+"/instances", map[string]any{
		"name":           name,
		"tenant_id":      c.tenantID,
		"cloud_provider": cloudProvider,
		"type":           instanceType,
		"memory":         memory,
		"version":        version,
		"region":         region,
	})
	if err != nil {
		return nil, err
	}
	apiResp, err := c.do(req)
	if err != nil {
		return nil, err
	}
	if apiResp.StatusCode < http.StatusOK || apiResp.StatusCode >= http.StatusMultipleChoices {
		return nil, newAuraError(errors.New(apiResp.Status), apiResp)
	}
	resp, err := newCreateResponse(apiResp)
	if err != nil {
		return nil, newAuraError(err, apiResp)
	}
	return resp, nil
}

// GetResponse contains information about a given Aura instance and
// is constructed from specification at
// https://neo4j.com/docs/aura/platform/api/specification/#/instances/get-instance-id.
type GetResponse struct {
	ID            string // Internal ID of the instance
	Name          string // The name we chose for the instance
	Status        string // Indicates whether the instance is ready or under setup
	TenantID      string // Tenant for managing Aura console users
	CloudProvider string // GCP, AWS, ...
	ConnectionURL string // URL the instance is hosted at
	Region        string // us-east1, eu-central2, ...
	InstanceType  string // enterprise-db, professional-db, ...
	Memory        string // Amount of memory allocated, i.e. "8GB"
	Storage       string // Amount of storage allocated, i.e. "16GB"
}

// newGetResponse attempts to construct a GetResponse struct from a given
// http response
func newGetResponse(httpResp *http.Response) (*GetResponse, error) {
	body, err := unmarshalResponse(httpResp)
	if err != nil {
		return nil, err
	}
	m, ok := body.(map[string]any)
	if !ok {
		return nil, errors.New(`expected response to be a map with string keys`)
	}
	resp := &GetResponse{}
	if resp.ID, ok = m["id"].(string); !ok {
		return nil, errors.New(`response missing key "id" or value not string`)
	}
	if resp.ConnectionURL, ok = m["connection_url"].(string); !ok {
		return nil, errors.New(`response missing key "connection_url" or value not string`)
	}
	if resp.Name, ok = m["name"].(string); !ok {
		return nil, errors.New(`response missing key "name" or value not string`)
	}
	if resp.TenantID, ok = m["tenant_id"].(string); !ok {
		return nil, errors.New(`response missing key "tenant_id" or value not string`)
	}
	if resp.CloudProvider, ok = m["cloud_provider"].(string); !ok {
		return nil, errors.New(`response missing key "cloud_provider" or value not string`)
	}
	if resp.Region, ok = m["region"].(string); !ok {
		return nil, errors.New(`response missing key "region" or value not string`)
	}
	if resp.InstanceType, ok = m["type"].(string); !ok {
		return nil, errors.New(`response missing key "type" or value not string`)
	}
	if resp.Status, ok = m["status"].(string); !ok {
		return nil, errors.New(`response missing key "status" or value not string`)
	}
	if resp.Memory, ok = m["memory"].(string); !ok {
		return nil, errors.New(`response missing key "memory" or value not string`)
	}
	if resp.Storage, ok = m["storage"].(string); !ok {
		return nil, errors.New(`response missing key "storage" or value not string`)
	}

	return resp, nil
}

// GetInstance attempts to get information about an instance identified
// by the ID assigned to it by Neo4J.
func (c *client) GetInstance(id string) (*GetResponse, error) {
	req, err := c.newRequest("GET", c.api()+"/instances/"+id, nil)
	if err != nil {
		return nil, err
	}
	apiResp, err := c.do(req)
	if err != nil {
		return nil, err
	}
	if apiResp.StatusCode < http.StatusOK || apiResp.StatusCode >= http.StatusMultipleChoices {
		return nil, newAuraError(errors.New(apiResp.Status), apiResp)
	}
	resp, err := newGetResponse(apiResp)
	if err != nil {
		return nil, newAuraError(err, apiResp)
	}
	return resp, nil
}

// PauseInstance puts a given instance on pause, making it unavailable for use.
// Note that you can only put instances on pause for a certain amount of time after which
// they automatically be put online again. Check the Aura documentation for details.
func (c *client) PauseInstance(id string) error {
	req, err := c.newRequest("POST", c.api()+"/instances/"+id+"/pause", nil)
	if err != nil {
		return err
	}
	apiResp, err := c.do(req)
	if err != nil {
		return err
	}
	if apiResp.StatusCode >= http.StatusOK && apiResp.StatusCode < http.StatusMultipleChoices {
		return nil
	}
	return newAuraError(errors.New(apiResp.Status), apiResp)
}

// Destroy instance tears down an instance identified by the Aura ID
// A 404 from the API is seen as succcesful as it indicates the instance no longer exists
func (c *client) DestroyInstance(id string) error {
	req, err := c.newRequest("DELETE", c.api()+"/instances/"+id, nil)
	if err != nil {
		return err
	}
	apiResp, err := c.do(req)
	if err != nil {
		return err
	}
	if apiResp.StatusCode == http.StatusNotFound ||
		(apiResp.StatusCode >= http.StatusOK && apiResp.StatusCode < http.StatusMultipleChoices) {
		return nil
	}
	return newAuraError(errors.New(apiResp.Status), apiResp)
}

// newRequest returns a request that is valid for the Neo4J Aura API
// given the HTTP method and path as well as a potential request body to
// add as a payload.
func (c *client) newRequest(method, path string, reqBody map[string]any) (*http.Request, error) {
	var body []byte
	var err error
	// Parse and add body
	if reqBody != nil {
		body, err = json.Marshal(reqBody)
		if err != nil {
			return nil, err
		}
	}
	req, err := http.NewRequest(method, path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}

	// Inject headers
	req.Header.Add("Content-Type", "application/json")
	req.Header.Add("Accept", "application/json")
	return req, nil
}

func (c *client) do(req *http.Request) (*http.Response, error) {
	// Perform the call
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	// Issue a warning if the endpoint is marked for deprecation
	if dep := resp.Header.Get("X-Tyk-Api-Expires"); dep != "" {
		c.logger.Warn(c.version + " of the Neo4J Aura API expires on " + dep + ".\nEncountered at " + req.URL.String())
	}
	return resp, err
}

func (c *client) api() string {
	return c.endpoint + "/" + c.version
}

// unmarshalResponse handles any API errors or returns the content
// in the `data` key of the API response. It assumes that the Aura API
// always returns content in maps with a single `data` key, i.e.
//
//	{
//		"data": response body goes here
//	}
func unmarshalResponse(resp *http.Response) (any, error) {
	var res map[string]any
	err := responseBodyToMap(resp, &res)
	if err != nil {
		return nil, err
	}
	if data, ok := res["data"]; ok {
		return data, nil
	}
	return nil, errors.New(`expected response to contain key "data".`)
}

func responseBodyToMap(resp *http.Response, res *map[string]any) error {
	defer resp.Body.Close()
	d := json.NewDecoder(resp.Body)
	return d.Decode(&res)
}

func responseBodyToString(resp *http.Response) string {
	if resp == nil {
		return ""
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil || len(body) == 0 {
		return ""
	}
	return string(body)
}
