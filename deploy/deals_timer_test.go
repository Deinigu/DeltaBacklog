package deploy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The Telegram settings the owner put in the installed settings file survive a deploy whose file does not carry them.
func TestMergeSettingsKeepsTelegram(t *testing.T) {
	dir := t.TempDir()
	newf, oldf := filepath.Join(dir, "new"), filepath.Join(dir, "old")
	os.WriteFile(newf, []byte("LISTEN_ADDR=:8081\nITAD_API_KEY=k\n"), 0o600)
	os.WriteFile(oldf, []byte("TELEGRAM_BOT_TOKEN=123456789:abc\nTELEGRAM_CHAT_ID=-1001\n"), 0o600)
	if out, err := run(t, "install.sh", "INSTALL_LIB", "merge_settings '"+filepath.ToSlash(newf)+"' '"+filepath.ToSlash(oldf)+"'"); err != nil {
		t.Fatalf("%v %q", err, out)
	}
	b, _ := os.ReadFile(newf)
	if want := "LISTEN_ADDR=:8081\nITAD_API_KEY=k\nTELEGRAM_BOT_TOKEN=123456789:abc\nTELEGRAM_CHAT_ID=-1001\n"; string(b) != want {
		t.Errorf("%q, want %q", b, want)
	}
	// So does the Steam id of the wishlist.
	os.WriteFile(oldf, []byte("STEAM_ID=76561198000000000\n"), 0o600)
	os.WriteFile(newf, []byte("ITAD_API_KEY=k\n"), 0o600)
	run(t, "install.sh", "INSTALL_LIB", "merge_settings '"+filepath.ToSlash(newf)+"' '"+filepath.ToSlash(oldf)+"'")
	if b, _ := os.ReadFile(newf); string(b) != "ITAD_API_KEY=k\nSTEAM_ID=76561198000000000\n" {
		t.Errorf("the installed Steam id was not kept: %q", b)
	}
	// A new value in the deploy's file wins over the installed one.
	os.WriteFile(newf, []byte("TELEGRAM_CHAT_ID=-2002\n"), 0o600)
	run(t, "install.sh", "INSTALL_LIB", "merge_settings '"+filepath.ToSlash(newf)+"' '"+filepath.ToSlash(oldf)+"'")
	if b, _ := os.ReadFile(newf); strings.Contains(string(b), "-1001") {
		t.Errorf("the installed chat id replaced the new one: %q", b)
	}
}

// The price check (every 6 hours) is shipped, installed and enabled like the nightly backup, and runs as the service user with the
// same settings file.
func TestDealsTimerIsInstalled(t *testing.T) {
	read := func(name string) string {
		b, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	svc, timer := read("deltabacklog-deals.service"), read("deltabacklog-deals.timer")
	for _, want := range []string{"Type=oneshot", "User=deltabacklog", "EnvironmentFile=/etc/deltabacklog.env", "ExecStart=/opt/deltabacklog/deltabacklog deals-check", "ReadWritePaths=/var/lib/deltabacklog"} {
		if !strings.Contains(svc, want) {
			t.Errorf("service lacks %q", want)
		}
	}
	for _, want := range []string{"OnCalendar=*-*-*", "Persistent=true", "RandomizedDelaySec=", "WantedBy=timers.target"} {
		if !strings.Contains(timer, want) {
			t.Errorf("timer lacks %q", want)
		}
	}
	install, deploy := read("install.sh"), read("deploy.sh")
	for _, want := range []string{`"$STAGE/deltabacklog-deals.service"`, `"$STAGE/deltabacklog-deals.timer"`, "enable deltabacklog.service deltabacklog-backup.timer deltabacklog-deals.timer", "start deltabacklog-backup.timer deltabacklog-deals.timer"} {
		if !strings.Contains(install, want) {
			t.Errorf("install.sh lacks %q", want)
		}
	}
	if !strings.Contains(deploy, "deploy/deltabacklog-deals.service deploy/deltabacklog-deals.timer") || !strings.Contains(deploy, "TELEGRAM_BOT_TOKEN|TELEGRAM_CHAT_ID") {
		t.Error("deploy.sh does not ship the timer or the Telegram settings")
	}
}
