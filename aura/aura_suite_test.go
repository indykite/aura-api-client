package aura_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestAura(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Aura Suite")
}
