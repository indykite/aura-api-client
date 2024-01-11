package aura_test

import (
	"encoding/json"
	"time"

	"github.com/indykite/aura-client/aura"
	"github.com/jarcoal/httpmock"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func mockAuth() {
	httpmock.RegisterResponder("POST", endpoint+"/oauth/token",
		httpmock.NewStringResponder(200, `{"access_token": "bar", "expires_in": 3600}`))
}

func mockGet(id string) {
	b := map[string]any{
		"data": map[string]any{
			"id":             id,
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
	httpmock.RegisterResponder("GET", endpoint+"/v1/instances/"+id,
		httpmock.NewStringResponder(200, string(body)))
}

func mockGetFailing(id string, errorCode int) {
	httpmock.RegisterResponder("GET", endpoint+"/v1/instances/"+id,
		httpmock.NewStringResponder(errorCode, "Some error"))
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
	httpmock.RegisterResponder("POST", endpoint+"/v1/instances",
		httpmock.NewStringResponder(200, string(body)))
}

func mockDestroy(id string) {
	httpmock.RegisterResponder("DELETE", endpoint+"/v1/instances/"+id,
		httpmock.NewStringResponder(202, "foo"))
}

func mockDestroyNotFound(id string) {
	httpmock.RegisterResponder("DELETE", endpoint+"/v1/instances/"+id,
		httpmock.NewStringResponder(404, "Not found"))
}

func mockDestroyFailing(id string) {
	httpmock.RegisterResponder("DELETE", endpoint+"/v1/instances/"+id,
		httpmock.NewStringResponder(409, "Busy"))
}

const endpoint = "aura.example"

var _ = Describe("Aura", func() {
	var (
		client aura.Client
		err    error
	)
	BeforeEach(func() {
		client, err = aura.NewClient("foo", "bar", "mox", aura.WithEndpoint(endpoint))
		if err != nil {
			panic(err)
		}
		mockAuth()
	})
	Describe("Authenticating", func() {
		It("should not be called when a valid token exists", func() {
			expiry := time.Now().Add(10 * time.Hour)
			client, err = aura.NewClient("foo", "bar", "mox",
				aura.WithEndpoint(endpoint),
				aura.WithAuthInfo("foo", expiry))
			mockGet("123id")
			_, err := client.GetInstance("123id")
			Expect(err).To(Succeed())
			calls := httpmock.GetCallCountInfo()
			Expect(calls["POST "+endpoint+"/oauth/token"]).To(Equal(0))
		})
		It("should retry on expired token", func() {
			expiry := time.Now().Add(-10 * time.Hour)
			client, err = aura.NewClient("foo", "bar", "mox",
				aura.WithEndpoint(endpoint),
				aura.WithAuthInfo("foo", expiry))
			mockGet("123id")
			_, err := client.GetInstance("123id")
			Expect(err).To(Succeed())
			calls := httpmock.GetCallCountInfo()
			Expect(calls["POST "+endpoint+"/oauth/token"]).To(Equal(1))
		})
		It("should be called when no token is present", func() {
			mockGet("123id")
			_, err := client.GetInstance("123id")
			Expect(err).To(Succeed())
			calls := httpmock.GetCallCountInfo()
			Expect(calls["POST "+endpoint+"/oauth/token"]).To(Equal(1))
		})
	})
	Describe("Retrying requests", func() {
		It("should not happen by default", func() {
			mockGetFailing("123id", 500)
			_, err := client.GetInstance("123id")
			Expect(err).NotTo(Succeed())
			calls := httpmock.GetCallCountInfo()
			Expect(calls["GET "+endpoint+"/v1/instances/123id"]).To(Equal(1))
		})
		It("should happen on some 5xx errors", func() {
			client, err = aura.NewClient("foo", "bar", "mox",
				aura.WithRetries(1),
				aura.WithEndpoint(endpoint))
			mockGetFailing("123id", 500)
			_, err := client.GetInstance("123id")
			Expect(err).NotTo(Succeed())
			calls := httpmock.GetCallCountInfo()
			Expect(calls["GET "+endpoint+"/v1/instances/123id"]).To(Equal(2))

		})
		It("should not happen on 501 or 4xx errors", func() {
			client, err = aura.NewClient("foo", "bar", "mox",
				aura.WithRetries(1),
				aura.WithEndpoint(endpoint))
			mockGetFailing("123id", 501)
			_, err := client.GetInstance("123id")
			Expect(err).NotTo(Succeed())
			calls := httpmock.GetCallCountInfo()
			Expect(calls["GET "+endpoint+"/v1/instances/123id"]).To(Equal(1))
		})
	})
	Describe("Creating an instance", func() {
		It("should create a post request to the Aura API", func() {
			mockCreate("foo")
			actual, err := client.CreateInstance("foo", "gcp", "2GB", "5", "us-east1", "enterprise-db")
			Expect(err).To(Succeed())
			Expect(actual.Name).To(Equal("foo"))
		})
	})
	Describe("Getting an instance", func() {
		It("should return the instance info when succesful", func() {
			mockGet("abc123")
			actual, err := client.GetInstance("abc123")
			Expect(err).To(Succeed())
			Expect(actual.ID).To(Equal("abc123"))
		})
	})
	Describe("Deleting an instance", func() {
		It("should return no error when successful", func() {
			mockDestroy("abc123")
			err := client.DestroyInstance("abc123")
			Expect(err).To(Succeed())
		})
		It("should treat 404 as success", func() {
			mockDestroyNotFound("abc123")
			err := client.DestroyInstance("abc123")
			Expect(err).To(Succeed())
		})
		It("should fail on other response codes", func() {
			mockDestroyFailing("abc123")
			err := client.DestroyInstance("abc123")
			Expect(err).To(Not(Succeed()))
		})
	})
})
