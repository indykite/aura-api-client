package aura_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"regexp"

	"github.com/indykite/aura-api-client/aura"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const responseId = "track-me-123"

type Path int64

const (
	CREATE_INSTANCE Path = iota
	DESTROY_INSTANCE
	GET_INSTANCE
	PAUSE_INSTANCE
	AUTHENTICATE
)

var callCounter map[Path]int

type F func(w http.ResponseWriter, r *http.Request) error

var responseMap map[Path]F

func authSuccess(w http.ResponseWriter, r *http.Request) error {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"access_token": "bar", "expires_in": 3600, "token_type": "Bearer"}`))
	return nil
}

func mockedGetResponse(id string) (int, []byte) {
	m := map[string]any{
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
			"storage":        "16GB"},
	}
	b, err := json.Marshal(m)
	if err != nil {
		panic(err)
	}
	return http.StatusOK, b
}

func mockError(code int) F {
	return func(w http.ResponseWriter, r *http.Request) error {
		m := map[string]any{
			"errors": []any{
				map[string]any{
					"message": "Server not responding.",
					"reason":  "It is on fire",
					"field":   "Ornithology",
				},
			},
		}
		_, err := json.Marshal(m)
		if err != nil {
			panic(err)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Request-Id", responseId)
		w.WriteHeader(code)
		_, _ = w.Write([]byte(`500 not working`))
		return nil
	}
}

func mockGet(id string) {
	f := func(w http.ResponseWriter, r *http.Request) error {
		code, b := mockedGetResponse(id)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Request-Id", responseId)
		w.WriteHeader(code)
		_, _ = w.Write(b)
		return nil
	}
	responseMap[GET_INSTANCE] = f
}

var _ = Describe("Aura", Ordered, func() {
	var (
		client aura.Client
		server *httptest.Server
		err    error
		routes map[Path]*regexp.Regexp
		pat    *regexp.Regexp
	)
	BeforeAll(func() {
		// Set up the routes object
		routes = make(map[Path]*regexp.Regexp)
		pat, err = regexp.Compile(`^\/oauth\/token$`)
		if err != nil {
			panic(err)
		}
		routes[AUTHENTICATE] = pat
		pat, err = regexp.Compile(`^\/v1\/instances\/\w+$`)
		if err != nil {
			panic(err)
		}
		routes[GET_INSTANCE] = pat
		pat, err = regexp.Compile(`^\/v1\/instances$`)
		if err != nil {
			panic(err)
		}
		routes[CREATE_INSTANCE] = pat
		pat, err = regexp.Compile(`^\/v1\/instances\/\w+$`)
		if err != nil {
			panic(err)
		}
		routes[DESTROY_INSTANCE] = pat
		pat, err = regexp.Compile(`^\/v1\/instances\/\w+\/pause$`)
		if err != nil {
			panic(err)
		}
		routes[PAUSE_INSTANCE] = pat
		// Create the server
		server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var path Path
			switch {
			case r.Method == "POST" && routes[AUTHENTICATE].Match([]byte(r.URL.Path)):
				path = AUTHENTICATE
			case r.Method == "GET" && routes[GET_INSTANCE].Match([]byte(r.URL.Path)):
				path = GET_INSTANCE
			case r.Method == "POST" && routes[CREATE_INSTANCE].Match([]byte(r.URL.Path)):
				path = CREATE_INSTANCE
			case r.Method == "DELETE" && routes[DESTROY_INSTANCE].Match([]byte(r.URL.Path)):
				path = DESTROY_INSTANCE
			case r.Method == "PUT" && routes[PAUSE_INSTANCE].Match([]byte(r.URL.Path)):
				path = PAUSE_INSTANCE
			default:
				panic("Unexpected request for testing")
			}
			err = responseMap[path](w, r)
			if err != nil {
				panic(err)
			}
			callCounter[path] += 1

		}))

	})
	BeforeEach(func() {
		responseMap = make(map[Path]F)
		callCounter = make(map[Path]int)
		client, err = aura.NewClient(context.Background(), "foo", "bar", "mox", aura.WithEndpoint(server.URL))
		if err != nil {
			panic(err)
		}
		responseMap[AUTHENTICATE] = authSuccess
	})
	Describe("Deprecation warnings", func() {
		var f F
		It("should be logged when found in the header", func() {
			var (
				b bytes.Buffer
			)
			id := "123id"
			depDate := "13. Nov 2026"
			h := slog.NewTextHandler(&b, nil)
			m := slog.New(h)
			client, err = aura.NewClient(context.Background(), "foo", "bar", "mox",
				aura.WithEndpoint(server.URL),
				aura.WithLogger(m),
			)
			// When the API is not deprecated nothing gets logged
			mockGet(id)
			_, err := client.GetInstance(id)
			Expect(err).To(Succeed())
			Expect(b.String()).NotTo(ContainSubstring(depDate))
			// When the API is deprecated we expect the deprecation date to get logged
			f = func(w http.ResponseWriter, r *http.Request) error {
				code, b := mockedGetResponse(id)
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("X-Request-Id", responseId)
				w.Header().Set("X-Tyk-Api-Expires", depDate)
				w.WriteHeader(code)
				_, _ = w.Write(b)
				return nil
			}
			responseMap[GET_INSTANCE] = f
			_, err = client.GetInstance(id)
			Expect(err).To(Succeed())
			Expect(b.String()).To(ContainSubstring(depDate))
		})
	})
	Describe("Request ID", func() {
		It("should be added from the response header", func() {
			// When the API is deprecated we expect the deprecation date to get logged
			responseMap[GET_INSTANCE] = mockError(500)
			_, err := client.GetInstance("123id")
			Expect(err).NotTo(Succeed())
			Expect(err.Error()).To(ContainSubstring(responseId))
		})
	})
	Describe("Authenticating", func() {
		It("should be called when no token is present and then cached", func() {
			mockGet("123id")
			_, err := client.GetInstance("123id")
			Expect(err).To(Succeed())
			Expect(callCounter[AUTHENTICATE]).To(Equal(1))
			_, err = client.GetInstance("123id")
			Expect(err).To(Succeed())
			Expect(callCounter[AUTHENTICATE]).To(Equal(1))
		})
	})
	Describe("Retrying requests", func() {
		It("should not happen by default", func() {
			responseMap[GET_INSTANCE] = mockError(500)
			_, err := client.GetInstance("123id")
			Expect(err).NotTo(Succeed())
			Expect(callCounter[GET_INSTANCE]).To(Equal(1))
		})
		It("should happen on some 5xx errors", func() {
			client, err = aura.NewClient(context.Background(), "foo", "bar", "mox",
				aura.WithRetries(1),
				aura.WithEndpoint(server.URL))
			responseMap[GET_INSTANCE] = mockError(500)
			_, err := client.GetInstance("123id")
			Expect(err).NotTo(Succeed())
			Expect(callCounter[GET_INSTANCE]).To(Equal(2))
		})
		It("should not happen on 501 or 4xx errors", func() {
			client, err = aura.NewClient(context.Background(), "foo", "bar", "mox",
				aura.WithRetries(1),
				aura.WithEndpoint(server.URL))
			responseMap[GET_INSTANCE] = mockError(501)
			_, err := client.GetInstance("123id")
			Expect(err).NotTo(Succeed())
			Expect(callCounter[GET_INSTANCE]).To(Equal(1))
		})
	})
	Describe("Creating an instance", func() {
		It("should create a post request to the Aura API", func() {
			f := func(w http.ResponseWriter, r *http.Request) error {
				m := map[string]any{
					"data": map[string]any{
						"id":             "db1d1234",
						"connection_url": "YOUR_CONNECTION_URL",
						"username":       "neo4j",
						"password":       "letMeIn123!",
						"tenant_id":      "YOUR_TENANT_ID",
						"cloud_provider": "gcp",
						"region":         "europe-west1",
						"type":           "enterprise-db",
						"name":           "foo",
					},
				}
				b, err := json.Marshal(m)
				if err != nil {
					panic(err)
				}
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("X-Request-Id", responseId)
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write(b)
				return nil
			}
			responseMap[CREATE_INSTANCE] = f
			actual, err := client.CreateInstance("foo", "gcp", "2GB", "5", "europe-west1", "enterprise-db")
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
		var f F
		It("should return no error when successful", func() {
			f = func(w http.ResponseWriter, r *http.Request) error {
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("X-Request-Id", responseId)
				w.WriteHeader(http.StatusAccepted)
				_, _ = w.Write([]byte(`202 OK`))
				return nil
			}
			responseMap[DESTROY_INSTANCE] = f
			err := client.DestroyInstance("abc123")
			Expect(err).To(Succeed())
		})
		It("should treat 404 as success", func() {
			f = func(w http.ResponseWriter, r *http.Request) error {
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("X-Request-Id", responseId)
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(`404 Not found`))
				return nil
			}
			responseMap[DESTROY_INSTANCE] = f
			err := client.DestroyInstance("abc123")
			Expect(err).To(Succeed())
		})
		It("should fail on other response codes", func() {
			f = func(w http.ResponseWriter, r *http.Request) error {
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("X-Request-Id", responseId)
				w.WriteHeader(http.StatusGone)
				_, _ = w.Write([]byte(`410 Gone`))
				return nil
			}
			responseMap[DESTROY_INSTANCE] = f
			err := client.DestroyInstance("abc123")
			Expect(err).NotTo(Succeed())
		})
	})
	Describe("Pausing an instance", func() {
		It("should create a PUT request to the right URL", func() {
			f := func(w http.ResponseWriter, r *http.Request) error {
				m := map[string]any{
					"data": map[string]any{
						"id":             "abc123",
						"name":           "Production",
						"status":         "pausing",
						"connection_url": "YOUR_CONNECTION_URL",
						"tenant_id":      "YOUR_TENANT_ID",
						"cloud_provider": "gcp",
						"memory":         "8GB",
						"region":         "europe-west1",
						"type":           "enterprise-db",
					},
				}
				b, err := json.Marshal(m)
				if err != nil {
					panic(err)
				}
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("X-Request-Id", responseId)
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write(b)
				return nil
			}
			responseMap[PAUSE_INSTANCE] = f
			err := client.PauseInstance("abc123")
			Expect(err).To(Succeed())
		})
	})
})
