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

type ResponseCommonProperties struct {
	ID            string `json:"id"`             // Internal ID of the instance
	Name          string `json:"name"`           // The name we chose for the instance
	TenantID      string `json:"tenant_id"`      // Tenant for managing Aura console users
	ConnectionURL string `json:"connection_url"` // URL the instance is hosted at
	CloudProvider string `json:"cloud_provider"` // GCP, AWS, ...
	Region        string `json:"region"`         // us-east1, eu-central2, ...
	InstanceType  string `json:"type"`           // enterprise-db, professional-db, ...
}

type CreateResponseData struct {
	ResponseCommonProperties
	Username string `json:"username"` // Name of the initial admin user
	Password string `json:"password"` // Password of the initial admin user
}

// CreateResponse is returned when creating an Aura instance and is
// constructed from the values from
// https://neo4j.com/docs/aura/platform/api/specification/#/instances/post-instances.
type CreateResponse struct {
	Data CreateResponseData `json:"data"`
}

type GetResponseData struct {
	ResponseCommonProperties
	Status  string `json:"status"`  // Indicates whether the instance is ready or under setup
	Memory  string `json:"memory"`  // Amount of memory allocated, i.e. "8GB"
	Storage string `json:"storage"` // Amount of storage allocated, i.e. "16GB"
}

// GetResponse contains information about a given Aura instance and
// is constructed from specification at
// https://neo4j.com/docs/aura/platform/api/specification/#/instances/get-instance-id.
type GetResponse struct {
	Data GetResponseData `json:"data"`
}

// CreateInstance attempts to create a new Aura instance with the given name
// returning information about the instance if successful and otherwise
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
	resp, err := c.do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, newAuraError(errors.New(resp.Status), resp)
	}

	var createResp CreateResponse
	err = json.NewDecoder(resp.Body).Decode(&createResp)
	if err != nil {
		return nil, newAuraError(err, resp)
	}

	return &createResp, nil
}

// GetInstance attempts to get information about an instance identified
// by the ID assigned to it by Neo4J.
func (c *client) GetInstance(id string) (*GetResponse, error) {
	req, err := c.newRequest("GET", c.api()+"/instances/"+id, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, newAuraError(errors.New(resp.Status), resp)
	}

	var getResp GetResponse
	err = json.NewDecoder(resp.Body).Decode(&getResp)
	if err != nil {
		return nil, newAuraError(err, resp)
	}

	return &getResp, nil
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
// A 404 from the API is seen as successful as it indicates the instance no longer exists
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
