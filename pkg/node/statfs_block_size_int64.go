//go:build linux && (amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || sparc64)

package node

import "golang.org/x/sys/unix"

// statfsBlockSize normalizes the platform-specific Statfs_t.Bsize type to int64.
func statfsBlockSize(statfs unix.Statfs_t) int64 {
	return statfs.Bsize
}
