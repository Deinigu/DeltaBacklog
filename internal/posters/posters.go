// Package posters gives every game a portrait box-art image (a ~300 px wide 2:3 JPEG next to its cover) for the
// rankings. The image comes from, in this order: the IGDB cover of the game's igdb_id, a SteamGridDB 600x900 grid,
// a centre crop of the wide cover that is already on disk. Downloads go through the same host allow-list and size
// limits as covers. A single background worker (Queue) fetches them one at a time, with a pause between games.
package posters

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Deinigu/DeltaBacklog/internal/covers"
	"github.com/Deinigu/DeltaBacklog/internal/meta"
)

// Source finds poster URLs. *meta.Client implements it; an unconfigured service returns an error.
type Source interface {
	IGDBCoverURL(ctx context.Context, igdbID int64) (string, error)
	SteamGridPortrait(ctx context.Context, title string) (string, error)
}

// Maker builds the poster of one game.
type Maker struct {
	DB      *sql.DB
	Dir     string                                              // the covers folder: posters live next to the covers
	Src     Source                                              // nil: no network source, only the crop of the cover
	Allowed func(rawURL string) bool                            // host allow-list for downloads; nil allows nothing
	Fetch   func(url, dir string, gameID int64) (string, error) // default covers.FetchPosterStrict

	// mu orders the final step of every maker (guarded UPDATE, then moving the file into place). The slow part
	// (lookups, downloads) runs outside it, into a scratch folder per call.
	mu sync.Mutex
}

// ErrNoSource means the game has no image to make a poster from.
var ErrNoSource = errors.New("no poster source")

// ErrUserPoster means the poster was chosen by the user: the automatic makers leave it alone.
var ErrUserPoster = errors.New("the poster was chosen by the user")

// ErrTemporary means a source failed in a way that may pass (a network error, a rate limit, a server error): no poster is
// made, not even the crop, so a later try can still find the real one. A crop is a permanent poster only when the
// sources really have nothing.
var ErrTemporary = errors.New("a poster source failed for now")

type gameRow struct {
	title      string
	igdbID     sql.NullInt64
	coverFile  sql.NullString
	posterFile sql.NullString
	posterSrc  sql.NullString
	posterUser bool
	posterAt   string // poster_updated_at when the row was read
}

func (m *Maker) row(id int64) (gameRow, error) {
	var g gameRow
	err := m.DB.QueryRow(`SELECT title, igdb_id, cover_file, poster_file, poster_src, poster_user, poster_updated_at FROM game WHERE id = ?`, id).
		Scan(&g.title, &g.igdbID, &g.coverFile, &g.posterFile, &g.posterSrc, &g.posterUser, &g.posterAt)
	return g, err
}

// Make creates (or replaces) the poster of a game and records it. It returns where the image came from
// ("igdb", "sgdb" or "crop"). A failing source is skipped for the next one; ErrNoSource when none worked.
// A poster the user chose is never replaced: ErrUserPoster.
func (m *Maker) Make(ctx context.Context, id int64) (string, error) { return m.make(ctx, id, false) }

// Remake is Make for "fetch it automatically again": it also replaces a poster the user chose, and only when a new
// one was really found (otherwise the old poster stays). The poster is automatic from then on.
func (m *Maker) Remake(ctx context.Context, id int64) (string, error) { return m.make(ctx, id, true) }

func (m *Maker) make(ctx context.Context, id int64, force bool) (string, error) {
	g, err := m.row(id)
	if err != nil {
		return "", err
	}
	if g.posterUser && !force {
		return "", ErrUserPoster
	}
	// The slow part (lookups, downloads) runs without the lock, into a scratch folder of its own; the lock is only
	// taken to decide whether the result may still go in (see commit).
	scratch, err := m.scratch()
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(scratch)
	fetch := m.Fetch
	if fetch == nil {
		fetch = covers.FetchPosterStrict
	}
	temporary := false
	try := func(src, url string) (string, bool) {
		if url == "" || m.Allowed == nil || !m.Allowed(url) {
			return "", false
		}
		name, err := fetch(url, scratch, id)
		if err != nil {
			log.Printf("posters: game %d: %s: %v", id, src, err)
			var se *covers.StatusError
			permanent := errors.Is(err, covers.ErrBadImage) || (errors.As(err, &se) && se.Permanent())
			if !permanent { // a bad image or a 404 will not get better: go on to the next source, then the crop
				temporary = true
			}
			return "", false
		}
		return name, true
	}
	var name, src string
	// A lookup error other than "not configured" is temporary; so is a failed download.
	lookupFailed := func(err error) {
		if !errors.Is(err, meta.ErrNotConfigured) {
			temporary = true
		}
	}
	if m.Src != nil && g.igdbID.Valid && g.igdbID.Int64 > 0 {
		if u, err := m.Src.IGDBCoverURL(ctx, g.igdbID.Int64); err != nil {
			log.Printf("posters: game %d: igdb: %v", id, err)
			lookupFailed(err)
		} else if n, ok := try("igdb", u); ok {
			name, src = n, "igdb"
		}
	}
	if name == "" && ctx.Err() == nil && m.Src != nil {
		if u, err := m.Src.SteamGridPortrait(ctx, g.title); err != nil {
			log.Printf("posters: game %d: steamgriddb: %v", id, err)
			lookupFailed(err)
		} else if n, ok := try("sgdb", u); ok {
			name, src = n, "sgdb"
		}
	}
	if name == "" && temporary {
		return "", ErrTemporary
	}
	if name == "" && g.coverFile.Valid && g.coverFile.String != "" {
		n, err := covers.CropPoster(filepath.Join(m.Dir, filepath.Base(g.coverFile.String)), scratch, id)
		if err != nil {
			log.Printf("posters: game %d: crop: %v", id, err)
		} else {
			name, src = n, "crop"
		}
	}
	if name == "" {
		return "", ErrNoSource
	}
	mode := commitAuto
	if force {
		mode = commitIfUnchanged
	}
	return src, m.commit(id, scratch, name, src, false, mode, g.posterAt)
}

// Choose makes the poster of a game from an image the user picked (url, checked against the allow-list and fetched
// like every poster) and marks it as theirs. The old poster stays when the download fails.
func (m *Maker) Choose(id int64, url string) error {
	if url == "" || m.Allowed == nil || !m.Allowed(url) {
		return ErrNotAllowed
	}
	fetch := m.Fetch
	if fetch == nil {
		fetch = covers.FetchPosterStrict
	}
	scratch, err := m.scratch()
	if err != nil {
		return err
	}
	defer os.RemoveAll(scratch)
	name, err := fetch(url, scratch, id)
	if err != nil {
		return err
	}
	return m.commit(id, scratch, name, sourceOf(url), true, commitAlways, "")
}

// ChooseCrop makes the poster a centre crop of the game's wide cover (no network) and marks it as the user's.
func (m *Maker) ChooseCrop(id int64) error {
	g, err := m.row(id)
	if err != nil {
		return err
	}
	if !g.coverFile.Valid || g.coverFile.String == "" {
		return ErrNoSource
	}
	scratch, err := m.scratch()
	if err != nil {
		return err
	}
	defer os.RemoveAll(scratch)
	name, err := covers.CropPoster(filepath.Join(m.Dir, filepath.Base(g.coverFile.String)), scratch, id)
	if err != nil {
		return err
	}
	return m.commit(id, scratch, name, "crop", true, commitAlways, "")
}

// ErrNotAllowed means the URL is not on the host allow-list.
var ErrNotAllowed = errors.New("image URL not allowed")

// sourceOf names where an allowed URL comes from (poster_src has no value for "somewhere else", so a host that is
// not IGDB counts as SteamGridDB).
func sourceOf(raw string) string {
	if u, err := url.Parse(raw); err == nil && strings.Contains(strings.ToLower(u.Hostname()), "igdb") {
		return "igdb"
	}
	return "sgdb"
}

// scratch is a private folder inside the covers folder (same file system, so the final rename is atomic) where one
// call writes its image; two calls never share a file, so nothing needs a lock while they download.
func (m *Maker) scratch() (string, error) {
	if err := os.MkdirAll(m.Dir, 0o755); err != nil {
		return "", err
	}
	return os.MkdirTemp(m.Dir, ".poster-")
}

// commit modes: what a finished poster may replace.
const (
	commitAuto        = iota // the queue and fetch-posters: never a poster the user chose
	commitIfUnchanged        // "fetch automatically again": a user poster too, unless the poster changed since it was read (a pick made meanwhile wins)
	commitAlways             // the user's own pick: the last pick wins
)

// commit records the finished poster and moves its file into place. The UPDATE is guarded by mode (see above), also
// against another process (the fetch-posters command) that chose its game before the user's pick. seen is the
// poster_updated_at the maker read first (commitIfUnchanged).
// The file only moves after the UPDATE took, so a refused result never overwrites the user's image. mu orders the
// UPDATE and the rename inside this process.
func (m *Maker) commit(id int64, scratch, name, src string, user bool, mode int, seen string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	flag := 0
	if user {
		flag = 1
	}
	guard := `poster_user = 0`
	args := []any{name, src, flag, id}
	switch mode {
	case commitAlways:
		guard = `1`
	case commitIfUnchanged:
		guard = `poster_user = 0 OR poster_updated_at = ?`
		args = append(args, seen)
	}
	res, err := m.DB.Exec(`UPDATE game SET poster_file = ?, poster_src = ?, poster_user = ?, poster_updated_at = strftime('%Y-%m-%dT%H:%M:%fZ','now')
		WHERE id = ? AND (`+guard+`)`, args...)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		var one int
		if m.DB.QueryRow(`SELECT 1 FROM game WHERE id = ?`, id).Scan(&one) == nil {
			return ErrUserPoster
		}
		return sql.ErrNoRows // the game was deleted meanwhile
	}
	return os.Rename(filepath.Join(scratch, name), filepath.Join(m.Dir, name))
}

// SweepScratch removes the scratch folders (.poster-*) that a crash left in the covers folder: only those older than
// maxAge, so a download in flight is never touched. It returns how many it removed.
func SweepScratch(dir string, maxAge time.Duration) int {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	n := 0
	for _, e := range entries {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), ".poster-") {
			continue
		}
		if info, err := e.Info(); err == nil && time.Since(info.ModTime()) > maxAge && os.RemoveAll(filepath.Join(dir, e.Name())) == nil {
			n++
		}
	}
	return n
}

// Queue is the background worker: one goroutine, one game at a time, a pause after each game.
type Queue struct {
	M      *Maker
	Pause  time.Duration // default 800 ms (IGDB allows 4 requests a second)
	OnMade func()        // called after a poster was stored (the server drops its cached game list)

	mu      sync.Mutex
	pending map[int64]bool
	failed  map[int64]bool // tried without result: not retried until the program restarts
	ch      chan int64
	once    sync.Once
}

func (q *Queue) start() {
	q.once.Do(func() {
		q.pending, q.failed = map[int64]bool{}, map[int64]bool{}
		q.ch = make(chan int64, 1024)
		go q.run()
	})
}

// Add queues the games that have no poster yet. It never blocks and never fails the caller; the queue is bounded.
func (q *Queue) Add(ids ...int64) {
	q.start()
	for _, id := range ids {
		q.mu.Lock()
		skip := q.pending[id] || q.failed[id]
		q.mu.Unlock()
		if skip {
			continue
		}
		var none int
		if err := q.M.DB.QueryRow(`SELECT 1 FROM game WHERE id = ? AND poster_file IS NULL`, id).Scan(&none); err != nil {
			continue
		}
		q.mu.Lock()
		q.pending[id] = true
		q.mu.Unlock()
		select {
		case q.ch <- id:
		default:
			q.mu.Lock()
			delete(q.pending, id)
			q.mu.Unlock()
		}
	}
}

// Pending counts how many of ids are waiting or being made right now.
func (q *Queue) Pending(ids []int64) int {
	q.start()
	q.mu.Lock()
	defer q.mu.Unlock()
	n := 0
	for _, id := range ids {
		if q.pending[id] {
			n++
		}
	}
	return n
}

func (q *Queue) run() {
	pause := q.Pause
	if pause == 0 {
		pause = 800 * time.Millisecond
	}
	for id := range q.ch {
		_, err := q.M.Make(context.Background(), id)
		if err == nil && q.OnMade != nil {
			q.OnMade()
		}
		q.mu.Lock()
		delete(q.pending, id)
		if err != nil {
			q.failed[id] = true // not tried again until the program restarts
		}
		q.mu.Unlock()
		if errors.Is(err, ErrTemporary) {
			time.Sleep(10 * pause) // something is wrong or busy over there: slow down
		}
		time.Sleep(pause)
	}
}

// describe is used by the dry run of the command line.
func describe(g gameRow) string {
	switch {
	case g.igdbID.Valid && g.igdbID.Int64 > 0:
		return fmt.Sprintf("IGDB cover (igdb_id %d), else SteamGridDB, else crop", g.igdbID.Int64)
	case g.coverFile.Valid && g.coverFile.String != "":
		return "SteamGridDB by title, else crop of the cover"
	default:
		return "SteamGridDB by title (no cover to crop)"
	}
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }
