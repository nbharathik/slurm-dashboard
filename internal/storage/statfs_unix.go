//go:build linux || darwin

package storage

import "syscall"

// SysStatFS reads a filesystem's size with statfs(2); no subprocess.
func SysStatFS(path string) (FSStat, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return FSStat{}, err
	}
	bs := uint64(st.Bsize) //nolint:gosec // block sizes are positive
	return FSStat{
		Total: uint64(st.Blocks) * bs, Free: uint64(st.Bfree) * bs, Avail: uint64(st.Bavail) * bs,
		Files: uint64(st.Files), FreeFiles: uint64(st.Ffree),
	}, nil
}
