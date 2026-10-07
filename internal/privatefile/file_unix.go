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

func safeDirectory(st os.FileInfo, leaf bool) bool {
	s, ok := st.Sys().(*syscall.Stat_t)
	if !ok {
		return false
	}
	if leaf {
		return s.Uid == uint32(os.Geteuid()) && st.Mode().Perm()&0o022 == 0 //nolint:gosec // effective UID is nonnegative
	}
	return (s.Uid == 0 || s.Uid == uint32(os.Geteuid())) && (st.Mode().Perm()&0o022 == 0 || st.Mode()&os.ModeSticky != 0) //nolint:gosec // effective UID is nonnegative
}

func openNoFollow(path string, flags int, mode os.FileMode) (*os.File, error) {
	fd, err := unix.Open(path, flags|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, uint32(mode))
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), path), nil
}
