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
	./csi-driver --components combined --endpoint 'unix:///tmp/anexia-csi-driver.sock' --nodeid $$(hostname) &
	sed -i "s/<storage-server-identifier>/$$ANEXIA_STORAGE_SERVER_IDENTIFIER/g" tests/sanity/volume-parameters.yaml
	go run github.com/kubernetes-csi/csi-test/v5/cmd/csi-sanity@latest \
		--csi.endpoint='unix:///tmp/anexia-csi-driver.sock' \
		--csi.testvolumeparameters='./tests/sanity/volume-parameters.yaml' \
		--csi.testvolumesize=1073741824

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
