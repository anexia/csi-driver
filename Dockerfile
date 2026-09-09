FROM golang:1.27-alpine AS builder
ARG version="v0.0.0-unreleased"

WORKDIR /src

COPY go.sum go.mod ./
RUN go mod download

COPY . .
RUN go build -ldflags "-s -w -X github.com/anexia/csi-driver/pkg/version.Version=$version" -trimpath ./cmd/csi-driver

FROM builder AS runtime-test-builder
RUN CGO_ENABLED=0 go test -c -tags=runtimeimage -o /controller.test ./pkg/controller

FROM alpine:3.24.1 AS runtime

# Keep nfs-utils pinned to its upstream version, but allow Alpine's package revision to differ by architecture.
# Pinning ca-certificates only gives us the downside of randomly failing Docker builds.
# Upgrade packages because security fixes can reach Alpine's stable repositories before refreshed image tags.
# hadolint ignore=DL3017,DL3018
RUN apk upgrade --no-cache && \
    apk add --no-cache ca-certificates coreutils 'nfs-utils=~2.6.4'

COPY --from=builder /src/csi-driver /csi-driver
ENTRYPOINT ["/csi-driver"]

# Test the production copier without installing tools that could replace it.
FROM runtime AS runtime-test
COPY --from=runtime-test-builder /controller.test /controller.test
ENTRYPOINT ["/controller.test", "-test.v", "-ginkgo.v", "-ginkgo.no-color", "-ginkgo.focus=runtime image copier"]

FROM runtime AS final
