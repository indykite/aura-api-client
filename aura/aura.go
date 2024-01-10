package aura

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const endpoint = "https://api.neo4j.io"

// Client is the interface containing the methods for connecting to the Aura API.
type Client interface {
	CreateInstance(name, cloudProvider, memory, version, region, instanceType string) (*CreateResponse, error)
	GetInstance(id string) (*GetResponse, error)
	DestroyInstance(id string) error
}

type client struct {
	httpClient     *http.Client
	endpoint       string
	accessToken    string
	tokenExpiresAt time.Time
	clientSecret   string
	clientID       string
	tenantID       string
}

type Option func(*client)

// NewClient creates a new based on a given client ID and secret as well as
// options for customizing the returned client.
func NewClient(clientID, clientSecret, tenantID string, options ...Option) (*client, error) {
	c := &client{
		httpClient:     &http.Client{},
		endpoint:       endpoint,
		clientID:       clientID,
		clientSecret:   clientSecret,
		tokenExpiresAt: time.Now(),
		tenantID:       tenantID,
	}
	for _, o := range options {
		o(c)
	}
	return c, nil
}

// WithHTTPClient sets the HTTP client used to communicate with Aura.
func WithHTTPClient(h http.Client) Option {
	return func(c *client) {
		c.httpClient = &h
	}
}

// WithEndpoint sets the a custom endpoint for Aura.
func WithEndpoint(e string) Option {
	return func(c *client) {
		c.endpoint = e
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

// NewCreateResponse attempts to construct a CreateResponse struct from a given
// http response
func NewCreateResponse(httpResp *http.Response) (*CreateResponse, error) {
	body, err := UnmarshalResponse(httpResp)
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
// Possible values for the parameters can be found in the documentation of the
// Neo4J Aura API
func (c *client) CreateInstance(name, cloudProvider, memory, version, region, instanceType string) (*CreateResponse, error) {
	req, err := c.NewRequest("POST", c.endpoint+"/v1/instances", map[string]any{
		"name":           name,
		"tenant_id":      c.tenantID,
		"cloud_provider": cloudProvider,
		"type":           instanceType, // "enterprise-db",
		"memory":         memory,       // "2GB",
		"version":        version,      // "5",
		"region":         region,       // "europe-west1",
	})
	if err != nil {
		return nil, err
	}
	apiResp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	if apiResp.StatusCode < http.StatusOK || apiResp.StatusCode >= http.StatusMultipleChoices {
		return nil, errors.New(apiResp.Status)
	}
	return NewCreateResponse(apiResp)
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

// NewGetResponse attempts to construct a GetResponse struct from a given
// http response
func NewGetResponse(httpResp *http.Response) (*GetResponse, error) {
	body, err := UnmarshalResponse(httpResp)
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
	req, err := c.NewRequest("GET", c.endpoint+"/v1/instances/"+id, nil)
	if err != nil {
		return nil, err
	}
	apiResp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	if apiResp.StatusCode < http.StatusOK || apiResp.StatusCode >= http.StatusMultipleChoices {
		return nil, errors.New(apiResp.Status)
	}
	return NewGetResponse(apiResp)
}

// Destroy instance tears down an instance identified by the Aura ID
// A 404 from the API is seen as succcesful as it indicates the instance no longer exists
func (c *client) DestroyInstance(id string) error {
	req, err := c.NewRequest("DELETE", c.endpoint+"/v1/instances/"+id, nil)
	if err != nil {
		return err
	}
	apiResp, err := c.Do(req)
	if err != nil {
		return err
	}
	if apiResp.StatusCode == http.StatusNotFound ||
		(apiResp.StatusCode >= http.StatusOK && apiResp.StatusCode < http.StatusMultipleChoices) {
		return nil
	}
	return errors.New(apiResp.Status)
}

// NewRequest returns a request that is valid for the Neo4J Aura API
// given the HTTP method and path as well as a potential request body to
// add as a payload.
func (c *client) NewRequest(method, path string, reqBody map[string]any) (*http.Request, error) {
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
	if body != nil {
		req.Header.Add("Content-Type", "application/json")
	}
	req.Header.Add("Accept", "application/json")
	return req, nil
}

// Do runs a given request and handles any authentication errors
// encountered along the way.
func (c *client) Do(req *http.Request) (*http.Response, error) {
	// Sign the request
	err := c.sign(req)
	if err != nil {
		return nil, err
	}
	// Perform the call
	resp, err := c.httpClient.Do(req)
	// If authorization is stale then refresh and call again
	if resp.StatusCode == http.StatusForbidden {
		// refresh bearer token
		c.accessToken = ""
		err = c.sign(req)
		if err != nil {
			return nil, err
		}
		resp, err = c.httpClient.Do(req)
	}
	return resp, err
}

// sign adds a valid access token to a request.
func (c *client) sign(req *http.Request) error {
	if c.accessToken == "" || time.Now().After(c.tokenExpiresAt) {
		err := c.authenticate()
		if err != nil {
			return err
		}
	}
	req.Header.Add("Authorization", "Bearer "+c.accessToken)
	return nil
}

// authenticate ensures that we have an access token and that it is valid.
func (c *client) authenticate() error {
	req, err := http.NewRequest("POST", c.endpoint+"/oauth/token", strings.NewReader("grant_type=client_credentials"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if err != nil {
		return err
	}
	req.SetBasicAuth(c.clientID, c.clientSecret)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return errors.New("authentication failed with status code: " + strconv.Itoa(resp.StatusCode))
	}
	var res map[string]any
	err = responseBodyToMap(resp, &res)
	if err != nil {
		return err
	}
	if _, ok := res["access_token"].(string); !ok {
		return errors.New("auth response missing access_token key or value not string")
	}
	if _, ok := res["expires_in"].(float64); !ok {
		return errors.New("auth response missing expires_in key or value not numerical")
	}
	c.accessToken = res["access_token"].(string)
	c.tokenExpiresAt = time.Now().Add(time.Second * time.Duration(int(res["expires_in"].(float64))))
	return nil
}

// UnmarshalResponse handles any API errors or returns the content
// in the `data` key of the API response. It assumes that the Aura API
// always returns content in maps with a single `data` key, i.e.
//
//	{
//		"data": response body goes here
//	}
func UnmarshalResponse(resp *http.Response) (any, error) {
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		// TODO add error handling
		return nil, nil
	}
	defer resp.Body.Close()
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
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if len(body) == 0 {
		return nil
	}
	d := json.NewDecoder(bytes.NewReader(body))
	return d.Decode(&res)
}
