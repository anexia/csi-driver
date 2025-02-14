csi-driver:
	go build ./cmd/csi-driver

test: hack
	go run github.com/onsi/ginkgo/v2/ginkgo \
		-p                                  \
		-timeout 0                          \
		-race                               \
		-coverpkg ./...                     \
		-coverprofile coverage.out          \
		--keep-going                        \
		./pkg/...
	go tool cover -html=coverage.out -o coverage.html

test-sanity: csi-driver
	tests/sanity/run.sh

depscheck:
	@hack/godepscheck.sh

fmt:
	gofmt -s -w .

fmtcheck:
	@hack/gofmtcheck.sh

.PHONY: csi-driver test test-sanity depscheck fmt
