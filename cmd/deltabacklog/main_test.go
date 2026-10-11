package main

import (
	"os"
	"path/filepath"
	"runtime/debug"
	"testing"
)

func TestParseMemLimit(t *testing.T) {
	ok := map[string]int64{"1024": 1024, "96MiB": 96 << 20, "1GiB": 1 << 30, "512KiB": 512 << 10, "10B": 10, " 2MiB ": 2 << 20}
	for in, want := range ok {
		if got, err := parseMemLimit(in); err != nil || got != want {
			t.Errorf("parseMemLimit(%q) = %d, %v; want %d", in, got, err, want)
		}
	}
	for _, in := range []string{"", "MiB", "-1MiB", "abc", "96MB", "9999999999999TiB"} {
		if _, err := parseMemLimit(in); err == nil {
			t.Errorf("parseMemLimit(%q) accepted", in)
		}
	}
}

func TestDotEnvAppliesGOMEMLIMIT(t *testing.T) {
	old := debug.SetMemoryLimit(-1)
	defer debug.SetMemoryLimit(old)
	t.Setenv("GOMEMLIMIT", "") // t.Setenv restores it; the empty value counts as unset for loadDotEnv
	path := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(path, []byte("GOMEMLIMIT=96MiB\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	loadDotEnv(path)
	if got := debug.SetMemoryLimit(-1); got != 96<<20 {
		t.Errorf("memory limit = %d, want %d", got, 96<<20)
	}
}

func TestScreenshotsDir(t *testing.T) {
	t.Setenv("SCREENSHOTS_DIR", "")
	if d, explicit := screenshotsDir("/data"); d != filepath.Join("/data", "screenshots") || explicit {
		t.Errorf("default: %q %v", d, explicit)
	}
	t.Setenv("SCREENSHOTS_DIR", "/mnt/drive/shots")
	if d, explicit := screenshotsDir("/data"); d != "/mnt/drive/shots" || !explicit {
		t.Errorf("explicit: %q %v", d, explicit)
	}
}

func TestEnvMB(t *testing.T) {
	for in, want := range map[string]int64{"": 25 << 20, "abc": 25 << 20, "-3": 25 << 20, "0": 25 << 20, "50": 50 << 20} {
		t.Setenv("T_MB", in)
		if got := envMB("T_MB", 25); got != want {
			t.Errorf("%q: %d, want %d", in, got, want)
		}
	}
}
