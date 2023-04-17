csi-driver:
	go build ./cmd/csi-driver

test: hack
	hack/ginkgo run -p              \
	    -timeout 0                  \
	    -race                       \
	    -coverprofile coverage.out  \
	    --keep-going                \
	    ./pkg/...
	go tool cover -html=coverage.out -o coverage.html

test-sanity: csi-driver
	tests/sanity/run.sh

hack:
	cd hack && go build -o . github.com/golangci/golangci-lint/cmd/golangci-lint
	cd hack && go build -o . github.com/onsi/ginkgo/v2/ginkgo

depscheck:
	@hack/godepscheck.sh

fmt:
	gofmt -s -w .

fmtcheck:
	@hack/gofmtcheck.sh

go-lint: hack
	@echo "==> Checking source code against linters..."
	@hack/golangci-lint run --timeout 5m ./...

.PHONY: csi-driver test test-sanity hack go-lint depscheck fmt
