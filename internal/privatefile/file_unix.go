//go:build unix

package privatefile

import (
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

func safeFile(st os.FileInfo) bool {
	s, ok := st.Sys().(*syscall.Stat_t)
	return ok && s.Uid == uint32(os.Geteuid()) && st.Mode().Perm()&0o077 == 0 //nolint:gosec // effective UID is nonnegative
}

func safeDirectory(st os.FileInfo) bool {
	return ownDirectory(st) && st.Mode().Perm()&0o022 == 0
}

func ownDirectory(st os.FileInfo) bool {
	s, ok := st.Sys().(*syscall.Stat_t)
	return ok && s.Uid == uint32(os.Geteuid()) //nolint:gosec // effective UID is nonnegative
}

func openNoFollow(path string, flags int, mode os.FileMode) (*os.File, error) {
	fd, err := unix.Open(path, flags|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, uint32(mode))
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), path), nil
}
