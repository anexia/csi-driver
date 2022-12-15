package node

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestNodeService(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "node service test suite")
}
