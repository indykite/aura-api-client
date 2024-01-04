package aura

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
)

const endpoint = "https://api.neo4j.io/v1"

type Client struct {
	Client       *http.Client
	bearerToken  string
	clientSecret string
	clientID     string
}

func NewClient(clientID, clientSecret string) (*Client, error) {
	return &Client{
		Client:       &http.Client{},
		clientID:     clientID,
		clientSecret: clientSecret,
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

func NewCreateResponse(httpResp *http.Response) (*CreateResponse, error) {
	body, err := UnmarshalResponse(httpResp)
	if err != nil {
		return nil, err
	}
	m, ok := body.(map[string]any)
	if !ok {
		return nil, errors.New(`Expected response to be a map with string keys`)
	}
	resp := &CreateResponse{}
	if resp.id, ok = m["id"].(string); !ok {
		return nil, errors.New(`Response missing key "id" or value not string`)
	}
	if resp.connection_url, ok = m["connection_url"].(string); !ok {
		return nil, errors.New(`Response missing key "connection_url" or value not string`)
	}
	if resp.username, ok = m["username"].(string); !ok {
		return nil, errors.New(`Response missing key "username" or value not string`)
	}
	if resp.password, ok = m["password"].(string); !ok {
		return nil, errors.New(`Response missing key "password" or value not string`)
	}
	if resp.name, ok = m["name"].(string); !ok {
		return nil, errors.New(`Response missing key "name" or value not string`)
	}
	if resp.tenant_id, ok = m["tenant_id"].(string); !ok {
		return nil, errors.New(`Response missing key "tenant_id" or value not string`)
	}
	if resp.cloud_provider, ok = m["cloud_provider"].(string); !ok {
		return nil, errors.New(`Response missing key "cloud_provider" or value not string`)
	}
	if resp.region, ok = m["region"].(string); !ok {
		return nil, errors.New(`Response missing key "region" or value not string`)
	}
	if resp.instance_type, ok = m["instance_type"].(string); !ok {
		return nil, errors.New(`Response missing key "instance_type" or value not string`)
	}
	return resp, nil
}

func (c *Client) CreateInstance() (*CreateResponse, error) {
	req, err := c.NewRequest("POST", endpoint+"/instances", nil)
	if err != nil {
		return nil, err
	}
	apiResp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	return NewCreateResponse(apiResp)
}

func (c *Client) DestroyInstance() {

}

func (c *Client) NewRequest(method, path string, reqBody map[string]any) (*http.Request, error) {
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

func (c *Client) Do(req *http.Request) (*http.Response, error) {
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
		c.bearerToken = ""
		err = c.sign(req)
		if err != nil {
			return nil, err
		}
		resp, err = c.Client.Do(req)
	}
	return resp, err
}

func (c *Client) sign(req *http.Request) error {
	if c.bearerToken == "" {
		err := c.authenticate()
		if err != nil {
			return err
		}
	}
	req.Header.Add("Authorization", "Bearer "+c.bearerToken)
	return nil
}

func (c *Client) authenticate() error {
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
		return errors.New("Authentication failed with status code: " + strconv.Itoa(resp.StatusCode))
	}
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
	defer resp.Body.Close()
	var res map[string]any
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		// TODO add error handling
		return nil, nil
	}
	d := json.NewDecoder(bytes.NewReader(body))
	err = d.Decode(&res)
	if err != nil {
		return nil, err
	}
	if data, ok := res["data"]; ok {
		// TODO add error handling
		return data, nil
	}
	return nil, errors.New(`Expected response to contain key "data"`)
}
