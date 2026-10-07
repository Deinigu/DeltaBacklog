// The edit form's state and the patch it produces. Pure (no DOM): unit-tested.

const YEAR = /^\d{4}$/;
const cap = (s) => s[0].toUpperCase() + s.slice(1);
const sameSet = (a, b) => a.length === b.length && a.every((x) => b.includes(x));

/** "Played in" as the strings the form shows: calendar years, then named periods ("Childhood", "Always"). */
export function yearsOf(game) {
  return [...game.years.map(String), ...game.periods.map(cap)];
}

/** The editable fields of a game, as form state (everything a text box can hold is a string). */
export function formFromGame(game) {
  return {
    title: game.title,
    platforms: [...game.platforms], genres: [...game.genres], themes: [...game.themes], players: [...game.players],
    series: game.series ?? '',
    series_order: game.series_order == null ? '' : String(game.series_order),
    developer: game.developer ?? '',
    release_year: game.release_year == null ? '' : String(game.release_year),
    ownership: game.ownership ?? '',
    years: yearsOf(game),
    notes: game.notes ?? '',
    vr: !!game.vr,
    completed: !!game.completed,
    igdb_id: game.igdb_id ?? null,
  };
}

/** The first problem with the form, or '' when it can be saved. */
export function validateForm(f) {
  if (!f.title.trim()) return 'A game needs a title.';
  const ry = String(f.release_year).trim();
  if (ry && (!YEAR.test(ry) || Number(ry) < 1950 || Number(ry) > 2100)) return 'The release year should be four digits, like 2025.';
  const so = String(f.series_order).trim();
  if (so && (!/^\d{1,4}$/.test(so) || Number(so) > 1000)) return 'The number in the series should be a whole number.';
  return '';
}

/** Only the fields that differ from the game, in the shape PATCH /api/games/{id} takes. {} when nothing changed. */
export function buildPatch(game, f) {
  const patch = {};
  const text = (key, current, next) => { if (next !== current) patch[key] = next; };

  text('title', game.title, f.title.trim());
  text('notes', game.notes ?? '', f.notes);
  text('series', game.series ?? '', f.series.trim());
  text('developer', game.developer ?? '', f.developer.trim());
  text('ownership', game.ownership ?? '', f.ownership);
  for (const k of ['platforms', 'genres', 'themes', 'players']) {
    const next = f[k].map((v) => v.trim()).filter(Boolean);
    if (!sameSet(next, game[k])) patch[k] = next;
  }

  const ry = String(f.release_year).trim() === '' ? 0 : Number(f.release_year);
  if (ry !== (game.release_year ?? 0)) patch.release_year = ry;
  const so = String(f.series_order).trim() === '' ? 0 : Number(f.series_order);
  if (so !== (game.series_order ?? 0)) patch.series_order = so;

  const years = f.years.filter((y) => YEAR.test(y)).map(Number);
  const periods = f.years.filter((y) => /^(childhood|always)$/i.test(y)).map((y) => y.toLowerCase());
  if (!sameSet(years, game.years)) patch.years = years;
  if (!sameSet(periods, game.periods)) patch.periods = periods;

  if (f.vr !== !!game.vr) patch.vr = f.vr;
  if (f.completed !== !!game.completed) patch.completed = f.completed;
  if ((f.igdb_id ?? null) !== (game.igdb_id ?? null)) patch.igdb_id = f.igdb_id ?? 0;
  return patch;
}

/** Which fields a chosen IGDB match fills in. Platforms, notes, status and ownership are never touched. */
export function applyCandidate(f, c) {
  return {
    ...f,
    genres: [...c.genres],
    themes: [...c.themes],
    developer: c.developer || f.developer,
    series: c.series || f.series,
    release_year: c.year ? String(c.year) : f.release_year,
    igdb_id: c.igdb_id,
  };
}

/** The values offered when picking years played: this year back to 1990, then the named periods. */
export function yearOptions(now = new Date().getFullYear()) {
  return [...Array.from({ length: now - 1990 + 1 }, (_, i) => String(now - i)), 'Childhood', 'Always'];
}

/** A typed value for the years picker: a four-digit year or a period name, normalised; null when it is neither. */
export function normalizeYearToken(t) {
  const s = String(t).trim();
  if (YEAR.test(s)) return s;
  return /^(childhood|always)$/i.test(s) ? cap(s.toLowerCase()) : null;
}
