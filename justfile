default:
    @just --list

csi-driver:
    go build ./cmd/csi-driver

test:
    go test -race ./deploy/...
    go run github.com/onsi/ginkgo/v2/ginkgo \
            -p \
            -timeout 0 \
            -race \
            -coverpkg ./... \
            -coverprofile coverage.out \
            --keep-going \
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
    #!/usr/bin/env bash
    set -euo pipefail
    echo "==> Checking source code dependencies..."
    go mod tidy
    if ! git diff --exit-code -- go.mod go.sum; then
        echo
        echo "Found differences in go.mod/go.sum files. Run 'go mod tidy' or revert go.mod/go.sum changes."
        exit 1
    fi
    git checkout -q go.sum

fmt:
    gofmt -s -w .

fmtcheck:
    #!/usr/bin/env bash
    set -euo pipefail
    echo "==> Checking that code complies with gofmt requirements..."
    files=$(gofmt -l $(find . -name '*.go' -not -path './vendor/*'))
    if [[ -n "$files" ]]; then
        echo "gofmt needs running on the following files:"
        echo "$files"
        echo "Run 'just fmt' to reformat code."
        exit 1
    fi

go-lint:
    golangci-lint run

vulncheck:
    go tool govulncheck ./...
