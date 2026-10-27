csi-driver:
	go build ./cmd/csi-driver

test:
	go test -race ./deploy/...
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

test-sanity-recovery:
	go test -race ./deploy/...
	go test -race ./pkg/controller -ginkgo.focus='Snapshot recovery sanity|undersized restore|independent bounded context'

test-snapshot-runtime:
	docker build --target runtime-test -t csi-driver-runtime-test .
	docker run --rm --tmpfs /tmp:rw,size=32m csi-driver-runtime-test

depscheck:
	@hack/godepscheck.sh

fmt:
	gofmt -s -w .

fmtcheck:
	@hack/gofmtcheck.sh

go-lint:
	golangci-lint run

.PHONY: csi-driver test test-sanity test-sanity-recovery test-snapshot-runtime depscheck fmt fmtcheck go-lint
