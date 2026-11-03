package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/Deinigu/DeltaBacklog/internal/db"
	"github.com/Deinigu/DeltaBacklog/internal/deals"
)

// telegramFromEnv builds the Telegram sender from TELEGRAM_BOT_TOKEN and TELEGRAM_CHAT_ID. It returns nil (alerts
// stay in the app) when neither is set or one is malformed; the log never holds the values.
func telegramFromEnv() deals.Notifier {
	t, err := deals.NewTelegram(os.Getenv("TELEGRAM_BOT_TOKEN"), os.Getenv("TELEGRAM_CHAT_ID"))
	switch {
	case errors.Is(err, deals.ErrBadToken):
		log.Print("Telegram alerts off: bad token format")
	case errors.Is(err, deals.ErrBadChatID):
		log.Print("Telegram alerts off: bad chat id format")
	case err != nil:
		log.Print("Telegram alerts off")
	case t != nil:
		return t
	}
	return nil
}

// runDealsCheck is `deltabacklog deals-check`: refresh the prices, apply the alert rule and send the alerts. It returns
// the exit code. It does not run while the app's own refresh (or another check) is running.
func runDealsCheck(dbPath string, src deals.Source, notify deals.Notifier, out io.Writer) int {
	if !src.Configured() {
		fmt.Fprintln(out, deals.ErrNotConfigured)
		return 1
	}
	if _, err := os.Stat(dbPath); err != nil {
		fmt.Fprintln(out, "no database at", filepath.ToSlash(dbPath))
		return 1
	}
	d, err := db.Open(dbPath)
	if err != nil {
		fmt.Fprintln(out, "cannot open the database:", err)
		return 1
	}
	defer d.Close()
	return checkDB(d, src, notify, out)
}

// How long the check waits for a refresh that is running (a manual one at the same time of day) before it gives up, and
// how often it looks. A skipped check would otherwise be lost until tomorrow.
var (
	lockWait = 20 * time.Minute
	lockPoll = 30 * time.Second
)

// acquire takes the run lock, waiting for lockWait when someone else has it. It reports to out what it does.
func acquire(d *sql.DB, out io.Writer) (func(), error) {
	deadline := time.Now().Add(lockWait)
	announced := false
	for {
		release, err := deals.AcquireRun(d, deals.HolderCheck)
		if err == nil {
			return release, nil
		}
		if !errors.Is(err, deals.ErrBusy) {
			fmt.Fprintln(out, "cannot start:", err)
			return nil, err
		}
		if !announced {
			fmt.Fprintln(out, "another price refresh is running: waiting for it to finish")
			announced = true
		}
		if time.Now().Add(lockPoll).After(deadline) {
			fmt.Fprintf(out, "gave up: a price refresh was still running after %s\n", lockWait)
			return nil, err
		}
		time.Sleep(lockPoll)
	}
}

func checkDB(d *sql.DB, src deals.Source, notify deals.Notifier, out io.Writer) int {
	release, err := acquire(d, out)
	if err != nil {
		return 1
	}
	defer release()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	res, err := deals.Check(ctx, d, src, notify)
	fmt.Fprintf(out, "prices: %s\n", res.Report)
	fmt.Fprintf(out, "%d new price %s", len(res.Alerts), map[bool]string{true: "alert", false: "alerts"}[len(res.Alerts) == 1])
	if res.Sent {
		fmt.Fprint(out, ", sent to Telegram")
	}
	fmt.Fprintln(out)
	if err != nil {
		fmt.Fprintln(out, "failed:", err) // the errors never carry a key or token
		return 1
	}
	return 0
}
