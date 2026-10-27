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

// A deploy's settings file (LISTEN_ADDR=:8081 and the keys) keeps the Pi-only settings of the installed one; with
// PUBLIC_NAME=off it also keeps the installed LISTEN_ADDR, and without it LISTEN_ADDR is the deploy's, as before.
func TestMergeSettings(t *testing.T) {
	dir := t.TempDir()
	merge := func(installed string) string {
		t.Helper()
		newf, oldf := filepath.Join(dir, "new"), filepath.Join(dir, "old")
		os.WriteFile(newf, []byte("LISTEN_ADDR=:8081\nDATA_DIR=/var/lib/deltabacklog\nIGDB_CLIENT_ID=abc\n"), 0o600)
		os.WriteFile(oldf, []byte(installed), 0o600)
		if out, err := run(t, "install.sh", "INSTALL_LIB", "merge_settings '"+filepath.ToSlash(newf)+"' '"+filepath.ToSlash(oldf)+"'"); err != nil {
			t.Fatalf("%v %q", err, out)
		}
		b, _ := os.ReadFile(newf)
		return string(b)
	}

	got := merge("LISTEN_ADDR=127.0.0.1:8081\nPUBLIC_NAME=off\nALLOWED_HOSTS=deltaraspberry.local\nSCREENSHOTS_DIR=/mnt/d/shots\nIGDB_CLIENT_ID=old\n")
	want := "DATA_DIR=/var/lib/deltabacklog\nIGDB_CLIENT_ID=abc\nSCREENSHOTS_DIR=/mnt/d/shots\nALLOWED_HOSTS=deltaraspberry.local\nPUBLIC_NAME=off\nLISTEN_ADDR=127.0.0.1:8081\n"
	if got != want {
		t.Errorf("PUBLIC_NAME=off:\n%q\nwant\n%q", got, want)
	}

	got = merge("LISTEN_ADDR=127.0.0.1:8081\nSCREENSHOTS_DIR=/mnt/d/shots\n")
	want = "LISTEN_ADDR=:8081\nDATA_DIR=/var/lib/deltabacklog\nIGDB_CLIENT_ID=abc\nSCREENSHOTS_DIR=/mnt/d/shots\n"
	if got != want {
		t.Errorf("without PUBLIC_NAME:\n%q\nwant\n%q", got, want)
	}
}

// PUBLIC_NAME=off disables and stops deltabacklog-name.service (its ExecStopPost removes the port 80 rules) instead of
// setting it up; absent or anything else, the friendly address is set up as before.
func TestNameUnit(t *testing.T) {
	dir := t.TempDir()
	log := filepath.Join(dir, "systemctl.log")
	bin := filepath.Join(dir, "bin")
	os.Mkdir(bin, 0o755)
	// the fake systemctl logs its arguments; is-enabled / is-active answer $UNIT_STATE (0: enabled and running)
	os.WriteFile(filepath.Join(bin, "systemctl"), []byte("#!/bin/sh\necho \"$*\" >> '"+filepath.ToSlash(log)+"'\n"+
		"case \"$1\" in is-enabled|is-active) exit \"${UNIT_STATE:-0}\" ;; esac\n"), 0o755)
	path := "PATH=" + filepath.ToSlash(bin) + ":" + os.Getenv("PATH")
	stub := "name_setup() { echo SETUP; }\n"
	env := filepath.Join(dir, "env")

	for _, c := range []struct {
		settings, state string
		setup           bool
		calls           string
	}{
		{"LISTEN_ADDR=127.0.0.1:8081\nPUBLIC_NAME=off\n", "0", false, "is-enabled --quiet deltabacklog-name.service\ndisable --now deltabacklog-name.service\n"},
		{"PUBLIC_NAME=\"off\"\n", "1", false, "is-enabled --quiet deltabacklog-name.service\nis-active --quiet deltabacklog-name.service\n"},
		{"LISTEN_ADDR=:8081\n", "0", true, ""},
		{"PUBLIC_NAME=on\n", "0", true, ""},
	} {
		os.WriteFile(env, []byte(c.settings), 0o600)
		os.Remove(log)
		out, err := run(t, "install.sh", "INSTALL_LIB", stub+"name_unit '"+filepath.ToSlash(env)+"'", path, "UNIT_STATE="+c.state)
		calls, _ := os.ReadFile(log)
		if err != nil || strings.Contains(out, "SETUP") != c.setup || string(calls) != c.calls {
			t.Errorf("%q (state %s): %v, output %q, systemctl calls %q", c.settings, c.state, err, out, calls)
		}
	}
}
