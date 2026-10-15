// The edit form's state and the patch it produces. Pure (no DOM): unit-tested.

const YEAR = /^\d{4}$/;
const MIN_YEAR = 1970; // the API accepts played years from here to MAX_YEAR
const MAX_YEAR = 2100;
const yearInRange = (s) => YEAR.test(s) && Number(s) >= MIN_YEAR && Number(s) <= MAX_YEAR;
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
    developer: game.developer ?? '',
    release_year: game.release_year == null ? '' : String(game.release_year),
    ownership: game.ownership ?? '',
    years: yearsOf(game),
    notes: game.notes ?? '',
    vr: !!game.vr,
    favorite: !!game.favorite,
    completed: !!game.completed,
    igdb_id: game.igdb_id ?? null,
  };
}

/** The first problem with the form, or '' when it can be saved. */
export function validateForm(f) {
  if (!f.title.trim()) return 'A game needs a title.';
  const ry = String(f.release_year).trim();
  if (ry && (!YEAR.test(ry) || Number(ry) < 1950 || Number(ry) > 2100)) return 'The release year should be four digits, like 2025.';
  if (f.years.some((y) => YEAR.test(y) && !yearInRange(y))) return `Years played must be between ${MIN_YEAR} and ${MAX_YEAR}.`;
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

  const years = f.years.filter(yearInRange).map(Number);
  const periods = f.years.filter((y) => /^(childhood|always)$/i.test(y)).map((y) => y.toLowerCase());
  if (!sameSet(years, game.years)) patch.years = years;
  if (!sameSet(periods, game.periods)) patch.periods = periods;

  if (f.vr !== !!game.vr) patch.vr = f.vr;
  if (f.favorite !== !!game.favorite) patch.favorite = f.favorite;
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

/** The calendar years the library's games were played in. */
export function libraryYears(games) {
  return [...new Set(games.flatMap((g) => g.years))];
}

/**
 * The values offered when picking years played: from the earliest year the library already uses up to this year
 * (the last 10 years while the library has none), then the named periods. A year a game already has is always
 * shown by the picker itself, and an older year can be typed in.
 */
export function yearOptions(now = new Date().getFullYear(), used = []) {
  const past = used.filter((y) => y >= MIN_YEAR && y <= now);
  const from = past.length ? Math.min(...past) : now - 9;
  return [...Array.from({ length: now - from + 1 }, (_, i) => String(now - i)), 'Childhood', 'Always'];
}

/** A typed value for the years picker: a four-digit year or a period name, normalised; null when it is neither. */
export function normalizeYearToken(t) {
  const s = String(t).trim();
  if (yearInRange(s)) return s;
  return /^(childhood|always)$/i.test(s) ? cap(s.toLowerCase()) : null;
}
