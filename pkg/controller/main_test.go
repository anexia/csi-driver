package controller

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestControllerService(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "controller service test suite")
}
