//go:build !linux

package shots

// FreeBytes is only known on Linux (the Pi); elsewhere the free-space margin is not enforced.
func FreeBytes(path string) (uint64, bool) { return 0, false }

// Writable cannot be asked without writing here: assume yes (uploads still run the real write probe).
func Writable(dir string) bool { return true }
