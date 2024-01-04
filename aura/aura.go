package aura

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"
)

const endpoint = "https://api.neo4j.io/v1"

// TODO: Add interface here
type Client interface {
	CreateInstance() (*CreateResponse, error)
}

type client struct {
	Client         *http.Client
	accessToken    string
	tokenExpiresAt time.Time
	clientSecret   string
	clientID       string
}

func NewClient(clientID, clientSecret string) (*client, error) {
	return &client{
		Client:         &http.Client{},
		clientID:       clientID,
		clientSecret:   clientSecret,
		tokenExpiresAt: time.Now(),
	}, nil
}

type CreateResponse struct {
	// Constructed from the values from
	// https://neo4j.com/docs/aura/platform/api/specification/#/instances/post-instances

	id             string // Internal ID of the instance
	connection_url string // URL the instance is hosted at
	username       string // Name of the initial admin user
	password       string // Password of the initial admin user
	name           string // The name we chose for the instance
	tenant_id      string // Tenant for managing Aura console users
	cloud_provider string // GCP, AWS, ...
	region         string // us-east1, eu-central2, ...
	instance_type  string // enterprise-db, professional-db, ...
}

// Clean this up with JSON tags
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
	if resp.id, ok = m["id"].(string); !ok {
		return nil, errors.New(`response missing key "id" or value not string`)
	}
	if resp.connection_url, ok = m["connection_url"].(string); !ok {
		return nil, errors.New(`response missing key "connection_url" or value not string`)
	}
	if resp.username, ok = m["username"].(string); !ok {
		return nil, errors.New(`response missing key "username" or value not string`)
	}
	if resp.password, ok = m["password"].(string); !ok {
		return nil, errors.New(`response missing key "password" or value not string`)
	}
	if resp.name, ok = m["name"].(string); !ok {
		return nil, errors.New(`response missing key "name" or value not string`)
	}
	if resp.tenant_id, ok = m["tenant_id"].(string); !ok {
		return nil, errors.New(`response missing key "tenant_id" or value not string`)
	}
	if resp.cloud_provider, ok = m["cloud_provider"].(string); !ok {
		return nil, errors.New(`response missing key "cloud_provider" or value not string`)
	}
	if resp.region, ok = m["region"].(string); !ok {
		return nil, errors.New(`response missing key "region" or value not string`)
	}
	if resp.instance_type, ok = m["instance_type"].(string); !ok {
		return nil, errors.New(`response missing key "instance_type" or value not string`)
	}
	return resp, nil
}

// CreateInstance should take a config object
func (c *client) CreateInstance(name string) (*CreateResponse, error) {
	req, err := c.NewRequest("POST", endpoint+"/instances", map[string]any{
		"name": name,
	})
	if err != nil {
		return nil, err
	}
	apiResp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	return NewCreateResponse(apiResp)
}

func (c *client) DestroyInstance() {

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
		req.Header.Add("Content-Type", "application/json;charset=utf-8")
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
	resp, err := c.Client.Do(req)
	// If authorization is stale then refresh and call again
	if resp.StatusCode == http.StatusForbidden {
		// refresh bearer token
		c.accessToken = ""
		err = c.sign(req)
		if err != nil {
			return nil, err
		}
		resp, err = c.Client.Do(req)
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
	body, err := json.Marshal(map[string]string{"grant_type": "client_credentials"})
	if err != nil {
		return err
	}
	req, err := http.NewRequest("POST", endpoint+"/oauth/token", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.SetBasicAuth(c.clientID, c.clientSecret)
	resp, err := c.Client.Do(req)
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
	// TODO switch to
	if _, ok := res["access_token"].(string); !ok {
		return errors.New("auth response missing access_token key or value not string")
	}
	if _, ok := res["expires_in"].(int); !ok {
		return errors.New("auth response missing expires_in key or value not integer")
	}
	c.accessToken = res["access_token"].(string)
	c.tokenExpiresAt = time.Now().Add(time.Second * time.Duration(res["expires_in"].(int)))
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
