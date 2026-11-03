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

func checkDB(d *sql.DB, src deals.Source, notify deals.Notifier, out io.Writer) int {
	release, err := deals.AcquireRun(d)
	if errors.Is(err, deals.ErrBusy) {
		fmt.Fprintln(out, "skipped: a price refresh is already running")
		return 0
	}
	if err != nil {
		fmt.Fprintln(out, "cannot start:", err)
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
