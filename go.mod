module github.com/anexia/csi-driver

go 1.26.0

toolchain go1.27.3

require (
	github.com/container-storage-interface/spec v1.13.0
	github.com/golang/mock v1.6.0
	github.com/hashicorp/go-multierror v1.1.1
	github.com/onsi/ginkgo/v2 v2.33.1
	github.com/onsi/gomega v1.44.0
	go.anx.io/go-anxcloud v0.14.5
	golang.org/x/sys v0.49.0
	google.golang.org/grpc v1.85.0
	k8s.io/klog/v2 v2.140.0
	k8s.io/mount-utils v0.37.2
)

require (
	go.yaml.in/yaml/v3 v3.0.5
	google.golang.org/protobuf v1.36.12
)

require (
	github.com/Masterminds/semver/v3 v3.4.0 // indirect
	github.com/go-logr/logr v1.4.4 // indirect
	github.com/go-task/slim-sprig/v3 v3.0.0 // indirect
	github.com/google/go-cmp v0.7.0 // indirect
	github.com/google/pprof v0.0.0-20260402051712-545e8a4df936 // indirect
	github.com/hashicorp/errwrap v1.1.0 // indirect
	github.com/moby/sys/mountinfo v0.7.2 // indirect
	golang.org/x/mod v0.42.0 // indirect
	golang.org/x/net v0.61.0 // indirect
	golang.org/x/sync v0.24.0 // indirect
	golang.org/x/telemetry v0.0.0-20261010193011-f71dc92da45e // indirect
	golang.org/x/text v0.43.0 // indirect
	golang.org/x/tools v0.52.0 // indirect
	golang.org/x/vuln v1.9.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260817212433-ac3dfec99bb1 // indirect
	k8s.io/utils v0.0.0-20260626114624-be93311217bd // indirect
)

tool golang.org/x/vuln/cmd/govulncheck
