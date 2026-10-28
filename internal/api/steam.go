package api

import (
	"context"
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/Deinigu/DeltaBacklog/internal/steam"
)

// steamConfigured: there is a Steam client with a key and an account.
func (s *Server) steamConfigured() bool { return s.Steam != nil && s.Steam.Configured() }

func (s *Server) steamError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, steam.ErrUnknownApp), errors.Is(err, steam.ErrUnknownGame):
		notFound(w)
	default:
		fail(w, err)
	}
}

// steamList answers the cached Steam apps (never Steam).
func (s *Server) steamList(w http.ResponseWriter, r *http.Request) {
	l, err := steam.Load(s.DB)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, map[string]any{"configured": s.steamConfigured(), "synced_at": l.SyncedAt, "apps": l.Apps, "refresh": s.steamJob.status()})
}

// steamSync starts reading the Steam library in the background; the answer is the job's status. One run at a time.
func (s *Server) steamSync(w http.ResponseWriter, r *http.Request) {
	if !s.steamConfigured() {
		writeErr(w, http.StatusServiceUnavailable, steam.ErrNotConfigured.Error(), nil)
		return
	}
	j := &s.steamJob
	j.mu.Lock()
	if j.running {
		j.mu.Unlock()
		writeJSONStatus(w, http.StatusAccepted, j.status())
		return
	}
	j.running, j.done, j.total, j.report, j.failed = true, 0, 0, "", false
	j.mu.Unlock()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		rep, err := steam.Sync(ctx, s.DB, s.Steam)
		j.mu.Lock()
		defer j.mu.Unlock()
		j.running = false
		j.finished = time.Now().UTC().Format(time.RFC3339)
		if err != nil {
			j.failed, j.report = true, steamMessage(err)
			if !isSteamUserError(err) {
				log.Printf("steam sync: %v", err)
			}
			return
		}
		j.report = rep.String()
	}()
	writeJSONStatus(w, http.StatusAccepted, j.status())
}

// isSteamUserError: the message is already written for the owner (private profile, bad key, rate limit).
func isSteamUserError(err error) bool {
	var rl *steam.RateLimitError
	return errors.Is(err, steam.ErrPrivate) || errors.Is(err, steam.ErrNotConfigured) || errors.As(err, &rl)
}

// steamMessage is what the page shows when a sync fails. The steam package never puts a URL or the key in an error.
func steamMessage(err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return "Steam took too long to answer."
	}
	if errors.Is(err, context.Canceled) {
		return "The sync was cancelled."
	}
	return err.Error()
}

type steamMatchBody struct {
	GameID int64 `json:"game_id"`
	None   bool  `json:"none"`
	Reset  bool  `json:"reset"`
}

// steamSetMatch records the owner's choice for a Steam app: a library game, "no library game", or back to automatic.
func (s *Server) steamSetMatch(w http.ResponseWriter, r *http.Request) {
	app, ok := pathInt(r, "app")
	if !ok {
		notFound(w)
		return
	}
	var in steamMatchBody
	if !readJSON(w, r, &in, 1<<10) {
		return
	}
	if n := b2n(in.GameID > 0) + b2n(in.None) + b2n(in.Reset); n != 1 || in.GameID < 0 {
		bad(w, "invalid body")
		return
	}
	var err error
	switch {
	case in.Reset:
		err = steam.ClearMatch(s.DB, app)
	case in.None:
		err = steam.SetNone(s.DB, app)
	default:
		err = steam.SetMatch(s.DB, app, in.GameID)
	}
	if err != nil {
		s.steamError(w, err)
		return
	}
	s.steamList(w, r)
}

// steamIgnore hides a Steam app from the review list, or shows it again.
func (s *Server) steamIgnore(w http.ResponseWriter, r *http.Request) {
	app, ok := pathInt(r, "app")
	if !ok {
		notFound(w)
		return
	}
	var in struct {
		Ignored bool `json:"ignored"`
	}
	if !readJSON(w, r, &in, 1<<10) {
		return
	}
	if err := steam.SetIgnored(s.DB, app, in.Ignored); err != nil {
		s.steamError(w, err)
		return
	}
	s.steamList(w, r)
}
