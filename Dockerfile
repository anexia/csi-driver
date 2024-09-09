FROM golang:1.23-alpine as builder
ARG version="v0.0.0-unreleased"

WORKDIR /src

COPY go.sum go.mod ./
RUN go mod download

COPY . .
RUN go build -ldflags "-s -w -X github.com/anexia/csi-driver/pkg/version.Version=$version" ./cmd/csi-driver

FROM alpine:3.20.3

# Hadolint wants us to pin apk packages to specific versions, mostly to make sure sudden incompatible changes
# don't get released - for ca-certificates this only gives us the downside of randomly failing docker builds
# hadolint ignore=DL3018
RUN apk --no-cache add ca-certificates && apk --no-cache add nfs-utils=2.6.4-r1

COPY --from=builder /src/csi-driver /csi-driver
ENTRYPOINT ["/csi-driver"]
