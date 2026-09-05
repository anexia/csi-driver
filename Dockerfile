FROM golang:1.26-alpine AS builder
ARG version="v0.0.0-unreleased"

WORKDIR /src

COPY go.sum go.mod ./
RUN go mod download

COPY . .
RUN go build -ldflags "-s -w -X github.com/anexia/csi-driver/pkg/version.Version=$version" -trimpath ./cmd/csi-driver

FROM alpine:3.24.1

# Keep nfs-utils pinned to its upstream version, but allow Alpine's package revision to differ by architecture.
# Pinning ca-certificates only gives us the downside of randomly failing Docker builds.
# Upgrade packages because security fixes can reach Alpine's stable repositories before refreshed image tags.
# hadolint ignore=DL3017,DL3018
RUN apk upgrade --no-cache && \
    apk add --no-cache ca-certificates 'nfs-utils=~2.6.4'

# Pull util-linux libraries from edge until Alpine 3.24 stable ships the patched version, then drop this step.
RUN apk add --no-cache -X https://dl-cdn.alpinelinux.org/alpine/edge/main \
    'libblkid>=2.42.3' 'libmount>=2.42.3' 'libuuid>=2.42.3'

COPY --from=builder /src/csi-driver /csi-driver
ENTRYPOINT ["/csi-driver"]
