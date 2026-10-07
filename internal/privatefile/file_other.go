//go:build !unix

package privatefile

import "os"

func safeFile(st os.FileInfo) bool              { return st.Mode()&os.ModeSymlink == 0 }
func safeDirectory(st os.FileInfo, _ bool) bool { return st.Mode()&os.ModeSymlink == 0 }
func openNoFollow(path string, flags int, mode os.FileMode) (*os.File, error) {
	if st, err := os.Lstat(path); err == nil && st.Mode()&os.ModeSymlink != 0 {
		return nil, ErrUnsafe
	}
	return os.OpenFile(path, flags, mode)
}
