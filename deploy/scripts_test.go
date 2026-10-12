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

// backup_shots hands the free-space margin to the program (it checks it file by file) and never calls it without the
// source folder.
func TestBackupShotsPassesTheMargin(t *testing.T) {
	src, dst := t.TempDir(), filepath.Join(t.TempDir(), "copy")
	os.WriteFile(filepath.Join(src, "a.png"), []byte("x"), 0o644)
	args := filepath.Join(t.TempDir(), "args")
	fake := t.TempDir()
	os.WriteFile(filepath.Join(fake, "deltabacklog"), []byte("#!/bin/sh\necho \"$@\" > '"+filepath.ToSlash(args)+"'\n"), 0o755)
	app := "APP_DIR=" + filepath.ToSlash(fake)

	if _, err := run(t, "backup.sh", "BACKUP_LIB", "backup_shots '"+filepath.ToSlash(src)+"' '"+filepath.ToSlash(dst)+"'", app, "SHOTS_MIN_FREE_GB=7"); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(args)
	if strings.TrimSpace(string(got)) != "backup-screenshots --min-free-gb 7 "+filepath.ToSlash(dst) {
		t.Errorf("arguments: %q", got)
	}
	os.Remove(args)
	out, err := run(t, "backup.sh", "BACKUP_LIB", "backup_shots '"+filepath.ToSlash(filepath.Join(src, "nope"))+"' '"+filepath.ToSlash(dst)+"'", app)
	if err != nil || !strings.Contains(out, "not available") {
		t.Errorf("absent drive: %v %q", err, out)
	}
	if _, err := os.Stat(args); err == nil {
		t.Error("the binary ran although the drive is absent")
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

// The systemd drop-ins: only for a folder outside the data folder, writable parent with the "-" prefix, ordering after
// the mount unit but no dependency on it; DROPIN_CHANGED tells install.sh to restart.
func TestShotsDropIn(t *testing.T) {
	units := t.TempDir()
	data := "/var/lib/deltabacklog"
	stub := "mount_unit() { echo mnt-ventodrive.mount; }\n"
	call := stub + "shots_dropin '/mnt/ventodrive/deltabacklog/screenshots' " + data + " '" + filepath.ToSlash(units) + "'\necho changed=$DROPIN_CHANGED"
	out, err := run(t, "install.sh", "INSTALL_LIB", call)
	if err != nil || !strings.Contains(out, "changed=1") {
		t.Fatalf("%v %q", err, out)
	}
	want := "[Unit]\nAfter=mnt-ventodrive.mount\n\n[Service]\nReadWritePaths=-/mnt/ventodrive/deltabacklog\n"
	for _, u := range []string{"deltabacklog", "deltabacklog-backup"} {
		got, _ := os.ReadFile(filepath.Join(units, u+".service.d", "screenshots.conf"))
		if string(got) != want {
			t.Errorf("%s drop-in: %q", u, got)
		}
		if strings.Contains(string(got), "RequiresMountsFor") || strings.Contains(string(got), "Requires=") || strings.Contains(string(got), "Wants=") {
			t.Error("the units must not depend on the mount")
		}
	}
	// running it again changes nothing, so no restart is asked for
	out, err = run(t, "install.sh", "INSTALL_LIB", call)
	if err != nil || !strings.Contains(out, "changed=0") {
		t.Errorf("second run: %v %q", err, out)
	}
	// another drive: changed again
	out, _ = run(t, "install.sh", "INSTALL_LIB", "mount_unit() { echo mnt-other.mount; }\nshots_dropin '/mnt/ventodrive/deltabacklog/screenshots' "+data+" '"+filepath.ToSlash(units)+"'\necho changed=$DROPIN_CHANGED")
	if !strings.Contains(out, "changed=1") {
		t.Errorf("a new mount unit should count as a change: %q", out)
	}
	// no known mount unit: no [Unit] section
	os.RemoveAll(units)
	run(t, "install.sh", "INSTALL_LIB", "mount_unit() { return 1; }\nshots_dropin '/mnt/ventodrive/deltabacklog/screenshots' "+data+" '"+filepath.ToSlash(units)+"'")
	if got, _ := os.ReadFile(filepath.Join(units, "deltabacklog.service.d", "screenshots.conf")); string(got) != "[Service]\nReadWritePaths=-/mnt/ventodrive/deltabacklog\n" {
		t.Errorf("without a mount unit: %q", got)
	}
	// back inside the data folder (or unset): the drop-ins go away, and that is a change
	for _, dir := range []string{"/var/lib/deltabacklog/screenshots", ""} {
		run(t, "install.sh", "INSTALL_LIB", call)
		out, err := run(t, "install.sh", "INSTALL_LIB", "shots_dropin '"+dir+"' "+data+" '"+filepath.ToSlash(units)+"'\necho changed=$DROPIN_CHANGED")
		if err != nil || !strings.Contains(out, "changed=1") {
			t.Fatalf("%v %q", err, out)
		}
		if _, err := os.Stat(filepath.Join(units, "deltabacklog.service.d")); err == nil {
			t.Errorf("drop-in kept for %q", dir)
		}
	}
}

// SCREENSHOTS_DIR equal to a mount point is refused: the backups folder would land on the SD card.
func TestMountPointAsScreenshotsDirIsRefused(t *testing.T) {
	stub := "mount_point_of() { case \"$1\" in /mnt/ventodrive|/mnt/ventodrive/*) echo /mnt/ventodrive ;; *) return 1 ;; esac; }\n"
	if _, err := run(t, "install.sh", "INSTALL_LIB", stub+"shots_is_mount /mnt/ventodrive"); err != nil {
		t.Error("the mount point itself should be refused")
	}
	if _, err := run(t, "install.sh", "INSTALL_LIB", stub+"shots_is_mount /mnt/ventodrive/deltabacklog/screenshots"); err == nil {
		t.Error("a folder inside the drive should be accepted")
	}
	if _, err := run(t, "install.sh", "INSTALL_LIB", stub+"shots_is_mount /somewhere/else"); err == nil {
		t.Error("a folder outside any drive is not a mount point")
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
