package aura_test

import (
	"encoding/json"

	"github.com/indykite/aura-client/aura"
	"github.com/jarcoal/httpmock"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func mockAuth() {
	httpmock.RegisterResponder("POST", endpoint+"/oauth/token",
		httpmock.NewStringResponder(200, `{"access_token": "bar", "expires_in": 3600}`))
}

func mockGet(ID string) {
	b := map[string]any{
		"data": map[string]any{
			"id":             ID,
			"name":           "Production",
			"status":         "running",
			"tenant_id":      "YOUR_TENANT_ID",
			"cloud_provider": "gcp",
			"connection_url": "YOUR_CONNECTION_URL",
			"region":         "europe-west1",
			"type":           "enterprise-db",
			"memory":         "8GB",
			"storage":        "16GB",
		},
	}
	body, _ := json.Marshal(b)
	httpmock.RegisterResponder("GET", endpoint+"/instances/"+ID,
		httpmock.NewStringResponder(200, string(body)))
}

func mockCreate(name string) {
	b := map[string]any{
		"data": map[string]any{
			"id":             "db1d1234",
			"connection_url": "YOUR_CONNECTION_URL",
			"username":       "neo4j",
			"password":       "letMeIn123!",
			"tenant_id":      "YOUR_TENANT_ID",
			"cloud_provider": "gcp",
			"region":         "europe-west1",
			"type":           "enterprise-db",
			"name":           name,
		},
	}
	body, _ := json.Marshal(b)
	httpmock.RegisterResponder("POST", endpoint+"/instances",
		httpmock.NewStringResponder(200, string(body)))
}

const endpoint = "aura.example"

var _ = Describe("Aura", func() {
	var (
		client aura.Client
		err    error
	)
	BeforeEach(func() {
		client, err = aura.NewClient("foo", "bar", aura.WithEndpoint(endpoint))
		if err != nil {
			panic(err)
		}
	})
	Describe("Creating an instance", func() {
		It("should create a post request to the Aura API", func() {
			mockAuth()
			mockCreate("foo")
			actual, err := client.CreateInstance("foo")
			Expect(err).To(Succeed())
			Expect(actual.Name).To(Equal("foo"))
		})
	})
	Describe("Getting an instance", func() {
		It("should return the instance info when succesful", func() {
			mockAuth()
			mockGet("abc123")
			actual, err := client.GetInstance("abc123")
			Expect(err).To(Succeed())
			Expect(actual.ID).To(Equal("abc123"))
		})
	})
	Describe("Deleting an instance", func() {
		It("should return the instance response when succesful", func() {

		})
		It("should treat 404 as success", func() {

		})
	})
})
