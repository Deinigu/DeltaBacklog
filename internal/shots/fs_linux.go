//go:build linux

package shots

import (
	"os"
	"path/filepath"
	"syscall"
)

// FreeBytes is the space available to this user on the filesystem holding path (the nearest existing parent when
// path does not exist yet).
func FreeBytes(path string) (uint64, bool) {
	for {
		var st syscall.Statfs_t
		err := syscall.Statfs(path, &st)
		if err == nil {
			return uint64(st.Bavail) * uint64(st.Bsize), true
		}
		parent := filepath.Dir(path)
		if !os.IsNotExist(err) || parent == path {
			return 0, false
		}
		path = parent
	}
}

// Writable asks the kernel whether dir can be written, without writing anything (a read-only list request must not
// wake a sleeping drive).
func Writable(dir string) bool { return syscall.Access(dir, 2) == nil }
