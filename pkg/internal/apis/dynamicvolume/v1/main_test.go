package v1

import (
	"testing"

	"go.anx.io/go-anxcloud/pkg/api/types"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ types.Object = &Volume{}
var _ types.Object = &StorageServerInterface{}

func TestControllerService(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "dynamic volume api bindings")
}
