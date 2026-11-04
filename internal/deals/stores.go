package deals

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode"
)

// Preferred stores: the owner ranks shops (Steam over Epic, say). The price kept for a game is the offer of the
// highest-ranked shop that sells it, not the cheapest one; a clearly cheaper offer elsewhere is kept as a hint (alt).
// An empty list means the cheapest offer, as before.

const (
	settingStores    = "deals_stores"
	settingThreshold = "deals_store_threshold"

	// DefaultThreshold is how much cheaper (percent) another shop must be to be shown as a hint.
	DefaultThreshold = 15
	// MaxStores is the longest ranking.
	MaxStores    = 30
	maxStoreName = 60
)

// StorePrefs is the ranking: Shops in order of preference, Threshold in percent.
type StorePrefs struct {
	Shops     []string `json:"stores"`
	Threshold int      `json:"threshold"`
}

// Active reports whether a ranking is set (otherwise prices are the cheapest offer, as ever).
func (p StorePrefs) Active() bool { return len(p.Shops) > 0 }

// ValidShopName: a plain shop name, as ITAD writes them ("Steam", "Humble Store", "GOG"). No markup or control characters.
func ValidShopName(s string) bool {
	if s != strings.TrimSpace(s) || s == "" || len([]rune(s)) > maxStoreName {
		return false
	}
	for _, r := range s {
		if unicode.IsControl(r) || strings.ContainsRune("<>\"`\\", r) {
			return false
		}
	}
	return true
}

// Validate checks a ranking from the API: at most MaxStores valid, distinct names (case aside) and a threshold of 0 to 100.
func (p StorePrefs) Validate() error {
	if len(p.Shops) > MaxStores {
		return fmt.Errorf("at most %d stores", MaxStores)
	}
	seen := map[string]bool{}
	for _, s := range p.Shops {
		if !ValidShopName(s) {
			return errors.New("invalid store name")
		}
		k := strings.ToLower(s)
		if seen[k] {
			return fmt.Errorf("%s is listed twice", s)
		}
		seen[k] = true
	}
	if p.Threshold < 0 || p.Threshold > 100 {
		return errors.New("threshold must be between 0 and 100")
	}
	return nil
}

// LoadStorePrefs reads the ranking; a missing or damaged setting means no ranking and the default threshold.
func LoadStorePrefs(d *sql.DB) StorePrefs {
	p := StorePrefs{Shops: []string{}, Threshold: DefaultThreshold}
	var shops []string
	if json.Unmarshal([]byte(settingValue(d, settingStores)), &shops) == nil && shops != nil {
		p.Shops = shops
	}
	if v := settingValue(d, settingThreshold); v != "" {
		var n int
		if _, err := fmt.Sscanf(v, "%d", &n); err == nil && n >= 0 && n <= 100 {
			p.Threshold = n
		}
	}
	if p.Validate() != nil {
		return StorePrefs{Shops: []string{}, Threshold: DefaultThreshold}
	}
	return p
}

// SaveStorePrefs validates and stores the ranking. Cached prices stay as they are until the next refresh.
func SaveStorePrefs(d *sql.DB, p StorePrefs) error {
	if p.Shops == nil {
		p.Shops = []string{}
	}
	if err := p.Validate(); err != nil {
		return err
	}
	b, _ := json.Marshal(p.Shops)
	tx, err := d.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for k, v := range map[string]string{settingStores: string(b), settingThreshold: fmt.Sprint(p.Threshold)} {
		if _, err := tx.Exec(`INSERT INTO setting(key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`, k, v); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// Choose applies the ranking to the offers of one game. chosen is the offer of the highest-ranked listed shop that has
// one; shops not listed rank below all listed ones, and among those (or when no listed shop sells it) the cheapest wins.
// alt is the cheapest offer overall when it is at another shop and more than Threshold percent cheaper than chosen.
func Choose(offers []Current, p StorePrefs) (chosen, alt *Current) {
	rank := map[string]int{}
	for i, s := range p.Shops {
		if _, ok := rank[strings.ToLower(s)]; !ok {
			rank[strings.ToLower(s)] = i
		}
	}
	rankOf := func(o *Current) int {
		if r, ok := rank[strings.ToLower(o.Shop)]; ok {
			return r
		}
		return len(p.Shops)
	}
	var best, cheapest *Current
	for i := range offers {
		o := &offers[i]
		if o.Price.Currency == "" {
			continue
		}
		if best == nil || rankOf(o) < rankOf(best) || (rankOf(o) == rankOf(best) && o.Price.Cents < best.Price.Cents) {
			best = o
		}
		if cheapest == nil || o.Price.Cents < cheapest.Price.Cents || (o.Price.Cents == cheapest.Price.Cents && rankOf(o) < rankOf(cheapest)) {
			cheapest = o
		}
	}
	if best == nil {
		return nil, nil
	}
	c := *best
	if cheapest.Price.Currency == c.Price.Currency && !strings.EqualFold(cheapest.Shop, c.Shop) && cheapest.Price.Cents < c.Price.Cents &&
		(c.Price.Cents-cheapest.Price.Cents)*100 > int64(p.Threshold)*c.Price.Cents {
		a := *cheapest
		return &c, &a
	}
	return &c, nil
}

// ShopSource is the optional per-shop price lookup (the real Client has it): every shop's current offer for many games in
// one request. Refreshes use it only when a ranking is set.
type ShopSource interface {
	// Offers reads the current offers of up to MaxOffersBatch games for a country, keyed by ITAD game id.
	Offers(ctx context.Context, ids []string, country string) (map[string][]Current, error)
}

// ShopLister is the optional list of shop names (for the Stores sheet).
type ShopLister interface {
	Shops(ctx context.Context, country string) ([]string, error)
}

// MaxOffersBatch is how many games one Offers call asks for.
const MaxOffersBatch = 200

// loadOffers reads the per-shop offers of all ids, 200 games per request. It returns nil (and calls nothing) when no
// ranking is set or the source cannot list offers: prices are then the overview's cheapest offer, as before.
func loadOffers(ctx context.Context, src Source, ids []string, country string, p StorePrefs) (map[string][]Current, error) {
	ss, ok := src.(ShopSource)
	if !ok || !p.Active() || len(ids) == 0 {
		return nil, nil
	}
	out := map[string][]Current{}
	for rest := ids; len(rest) > 0; {
		n := min(len(rest), MaxOffersBatch)
		got, err := ss.Offers(ctx, rest[:n], country)
		if err != nil {
			return nil, err
		}
		for k, v := range got {
			out[k] = v
		}
		rest = rest[n:]
	}
	return out, nil
}

// withChoice puts the ranking's choice in an overview: Current becomes the chosen offer, Alt the hint. The all-time low
// stays as ITAD gave it. A game with no offers keeps the overview as it is.
func withChoice(o Overview, offers []Current, p StorePrefs) Overview {
	if len(offers) == 0 {
		return o
	}
	chosen, alt := Choose(offers, p)
	if chosen == nil {
		return o
	}
	o.Current, o.Alt = chosen, alt
	return o
}
