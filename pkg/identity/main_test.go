package identity

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestIdentityService(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "identity service test suite")
}
