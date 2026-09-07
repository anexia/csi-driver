//go:build darwin || (linux && (386 || arm || mips || mipsle || ppc || s390x))

package node

import "golang.org/x/sys/unix"

// statfsBlockSize normalizes the platform-specific Statfs_t.Bsize type to int64.
func statfsBlockSize(statfs unix.Statfs_t) int64 {
	return int64(statfs.Bsize)
}
