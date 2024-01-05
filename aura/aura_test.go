package aura_test

import (
	"net/http"

	"github.com/indykite/aura-client/aura"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const endpoint = "aura.example"

func handler(w http.ResponseWriter, r *http.Request) {

}

var _ = Describe("Aura", func() {
	BeforeEach(func() {
		client, err := &aura.NewClient("foo", "bar", aura.WithEndpoint(endpoint))
		if err != nil {
			panic(err)
		}
	})
	Describe("Creating an instance", func() {
		It("should create a post request to the Aura API", func() {

		})
	})
})
