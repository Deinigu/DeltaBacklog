package deploy

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func needSh(t *testing.T) string {
	t.Helper()
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("no sh on this machine")
	}
	return sh
}

// run executes a shell snippet that first sources a script in library mode and returns its output.
func run(t *testing.T, lib, libVar, snippet string, env ...string) (string, error) {
	t.Helper()
	sh := needSh(t)
	script, _ := filepath.Abs(lib)
	cmd := exec.Command(sh, "-c", ". '"+filepath.ToSlash(script)+"'\n"+snippet)
	cmd.Env = append(os.Environ(), append([]string{libVar + "=1"}, env...)...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func TestScriptsParse(t *testing.T) {
	sh := needSh(t)
	files, _ := filepath.Glob("*.sh")
	for _, f := range files {
		if out, err := exec.Command(sh, "-n", f).CombinedOutput(); err != nil {
			t.Errorf("%s: %v\n%s", f, err, out)
		}
	}
}

// The SD card keeps a free-space margin: room_for says no when the copy would leave less than the minimum.
func TestFreeSpaceGuard(t *testing.T) {
	// 10 GiB free
	pre := "free_kb() { echo 10485760; }\n"
	for _, c := range []struct {
		needKB, minGB string
		want          bool
	}{
		{"1048576", "5", true},  // 1 GiB copy leaves 9
		{"5242880", "5", true},  // leaves exactly 5
		{"5242881", "5", false}, // one KiB too many
		{"0", "11", false},      // margin larger than the disk
		{"0", "5", true},
	} {
		_, err := run(t, "backup.sh", "BACKUP_LIB", pre+"room_for /x "+c.needKB+" "+c.minGB)
		if got := err == nil; got != c.want {
			t.Errorf("need %s KiB, margin %s GB: %v, want %v", c.needKB, c.minGB, got, c.want)
		}
	}
}

// With no room, backup_shots logs and copies nothing; the binary is never called.
func TestBackupShotsSkipsWhenFullOrAbsent(t *testing.T) {
	src, dst := t.TempDir(), filepath.Join(t.TempDir(), "copy")
	os.WriteFile(filepath.Join(src, "a.png"), []byte("x"), 0o644)
	marker := filepath.Join(t.TempDir(), "called")
	fake := t.TempDir()
	os.WriteFile(filepath.Join(fake, "deltabacklog"), []byte("#!/bin/sh\ntouch '"+filepath.ToSlash(marker)+"'\n"), 0o755)
	app := "APP_DIR=" + filepath.ToSlash(fake)

	out, err := run(t, "backup.sh", "BACKUP_LIB", "free_kb() { echo 1000; }\nbackup_shots '"+filepath.ToSlash(src)+"' '"+filepath.ToSlash(dst)+"'", app)
	if err != nil || !strings.Contains(out, "skipped") {
		t.Errorf("full disk: %v %q", err, out)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Error("the binary ran although there was no room")
	}
	out, err = run(t, "backup.sh", "BACKUP_LIB", "backup_shots '"+filepath.ToSlash(filepath.Join(src, "nope"))+"' '"+filepath.ToSlash(dst)+"'", app)
	if err != nil || !strings.Contains(out, "not available") {
		t.Errorf("absent drive: %v %q", err, out)
	}
	// plenty of room: the binary is called
	if _, err := run(t, "backup.sh", "BACKUP_LIB", "free_kb() { echo 99999999; }\nbackup_shots '"+filepath.ToSlash(src)+"' '"+filepath.ToSlash(dst)+"'", app); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Error("the binary was not called although there was room")
	}
}

func TestDatabaseCopyToTheDrive(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "app-2026-01-01.db")
	os.WriteFile(db, []byte("db"), 0o644)
	drive := filepath.Join(dir, "backups")
	os.Mkdir(drive, 0o755)
	if _, err := run(t, "backup.sh", "BACKUP_LIB", "keep_copy '"+filepath.ToSlash(db)+"' '"+filepath.ToSlash(drive)+"'"); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(drive, "app-2026-01-01.db")); string(b) != "db" {
		t.Errorf("copy = %q", b)
	}
	// the folder is never created: no drive, no copy
	missing := filepath.Join(dir, "no-drive", "backups")
	out, err := run(t, "backup.sh", "BACKUP_LIB", "keep_copy '"+filepath.ToSlash(db)+"' '"+filepath.ToSlash(missing)+"'")
	if err != nil || !strings.Contains(out, "not available") {
		t.Errorf("%v %q", err, out)
	}
	if _, err := os.Stat(filepath.Dir(missing)); err == nil {
		t.Error("created the missing drive folder")
	}
}

func TestEnvValue(t *testing.T) {
	f := filepath.Join(t.TempDir(), "env")
	os.WriteFile(f, []byte("A=1\n#SCREENSHOTS_DIR=/old\nSCREENSHOTS_DIR=\"/mnt/d/shots\"\nSCREENSHOTS_DIR_X=no\n"), 0o600)
	out, err := run(t, "install.sh", "INSTALL_LIB", "env_value '"+filepath.ToSlash(f)+"' SCREENSHOTS_DIR")
	if err != nil || strings.TrimSpace(out) != "/mnt/d/shots" {
		t.Errorf("%v %q", err, out)
	}
	out, _ = run(t, "install.sh", "INSTALL_LIB", "env_value '"+filepath.ToSlash(f)+"' NOPE")
	if strings.TrimSpace(out) != "" {
		t.Errorf("%q", out)
	}
}

// The systemd drop-ins: only for a folder outside the data folder, with the "-" prefix and no mount dependency.
func TestShotsDropIn(t *testing.T) {
	units := t.TempDir()
	data := "/var/lib/deltabacklog"
	call := "shots_dropin '/mnt/ventodrive/deltabacklog/screenshots' " + data + " '" + filepath.ToSlash(units) + "'"
	if _, err := run(t, "install.sh", "INSTALL_LIB", call); err != nil {
		t.Fatal(err)
	}
	main, _ := os.ReadFile(filepath.Join(units, "deltabacklog.service.d", "screenshots.conf"))
	bk, _ := os.ReadFile(filepath.Join(units, "deltabacklog-backup.service.d", "screenshots.conf"))
	if string(main) != "[Service]\nReadWritePaths=-/mnt/ventodrive/deltabacklog/screenshots\n" {
		t.Errorf("main drop-in: %q", main)
	}
	if string(bk) != "[Service]\nReadWritePaths=-/mnt/ventodrive/deltabacklog\n" { // also writes <parent>/backups
		t.Errorf("backup drop-in: %q", bk)
	}
	if strings.Contains(string(main)+string(bk), "RequiresMountsFor") {
		t.Error("the units must not depend on the mount")
	}
	// idempotent
	if _, err := run(t, "install.sh", "INSTALL_LIB", call); err != nil {
		t.Fatal(err)
	}
	// back inside the data folder (or unset): the drop-ins go away
	for _, dir := range []string{"/var/lib/deltabacklog/screenshots", ""} {
		run(t, "install.sh", "INSTALL_LIB", call)
		if _, err := run(t, "install.sh", "INSTALL_LIB", "shots_dropin '"+dir+"' "+data+" '"+filepath.ToSlash(units)+"'"); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(filepath.Join(units, "deltabacklog.service.d")); err == nil {
			t.Errorf("drop-in kept for %q", dir)
		}
	}
}

func TestOnOtherMountRefusesRoot(t *testing.T) {
	needSh(t)
	if _, err := exec.LookPath("findmnt"); err != nil {
		t.Skip("no findmnt")
	}
	if _, err := run(t, "install.sh", "INSTALL_LIB", "on_other_mount /"); err == nil {
		t.Error("/ is not another mount")
	}
	if _, err := run(t, "install.sh", "INSTALL_LIB", "on_other_mount /definitely/not/here/shots"); err == nil {
		t.Error("a path that resolves to / is not another mount")
	}
}
