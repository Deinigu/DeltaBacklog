import test from 'node:test';
import assert from 'node:assert/strict';
import { yearsOf, formFromGame, validateForm, buildPatch, applyCandidate, yearOptions, normalizeYearToken } from '../static/js/editpatch.js';

const game = () => ({
  id: 1, title: 'Hades II', notes: 'line one\nline two', series: 'Hades', developer: 'Supergiant', release_year: 2025,
  ownership: 'Owned', platforms: ['PC', 'Switch'], genres: ['RPG', 'Indie'], themes: ['Action'], players: ['Solo'],
  years: [2025, 2026], periods: ['childhood'], vr: false, completed: false, igdb_id: null,
});

test('an untouched form produces an empty patch', () => {
  assert.deepEqual(buildPatch(game(), formFromGame(game())), {});
});

test('years played show years first, then capitalised periods', () => {
  assert.deepEqual(yearsOf(game()), ['2025', '2026', 'Childhood']);
});

test('only changed fields are sent; list order does not matter', () => {
  const f = formFromGame(game());
  f.platforms = ['Switch', 'PC'];           // same set, different order: not a change
  f.genres = ['Indie', 'RPG', 'Roguelike']; // a real change
  f.title = '  Hades 2 ';
  const p = buildPatch(game(), f);
  assert.deepEqual(p, { title: 'Hades 2', genres: ['Indie', 'RPG', 'Roguelike'] });
});

test('years and periods are split, and only a changed half is sent', () => {
  const f = formFromGame(game());
  f.years = ['2025', '2026', '2027', 'Childhood'];
  assert.deepEqual(buildPatch(game(), f), { years: [2025, 2026, 2027] });
  f.years = ['2025', '2026', 'always'];
  assert.deepEqual(buildPatch(game(), f), { periods: ['always'] });
});

test('clearing text and numbers sends the clearing values the API expects', () => {
  const f = formFromGame(game());
  f.series = ''; f.release_year = ''; f.developer = ''; f.ownership = ''; f.notes = '';
  assert.deepEqual(buildPatch(game(), f), { series: '', release_year: 0, developer: '', ownership: '', notes: '' });
});

test('notes keep their line breaks and are compared exactly', () => {
  const f = formFromGame(game());
  f.notes = 'line one\nline two\n';
  assert.deepEqual(buildPatch(game(), f), { notes: 'line one\nline two\n' });
});

test('flags and the IGDB link', () => {
  const f = formFromGame(game());
  f.vr = true; f.completed = true; f.igdb_id = 228525;
  assert.deepEqual(buildPatch(game(), f), { vr: true, completed: true, igdb_id: 228525 });
  const linked = { ...game(), igdb_id: 5 };
  const g = formFromGame(linked); g.igdb_id = null;
  assert.deepEqual(buildPatch(linked, g), { igdb_id: 0 }); // 0 clears
});

test('validation catches the mistakes the API would reject', () => {
  const f = formFromGame(game());
  assert.equal(validateForm(f), '');
  assert.match(validateForm({ ...f, title: '   ' }), /title/);
  assert.match(validateForm({ ...f, release_year: '25' }), /four digits/);
  assert.match(validateForm({ ...f, release_year: '1800' }), /four digits/);
  assert.equal(validateForm({ ...f, release_year: '' }), '');
});

test('filling from IGDB replaces metadata but keeps platforms, notes and ownership', () => {
  const f = formFromGame(game());
  f.notes = 'mine';
  const filled = applyCandidate(f, { igdb_id: 99, genres: ['Adventure'], themes: [], developer: '', series: '', year: 2019 });
  assert.deepEqual(filled.genres, ['Adventure']);
  assert.deepEqual(filled.themes, []);
  assert.equal(filled.developer, 'Supergiant', 'an empty IGDB value does not erase what is there');
  assert.equal(filled.series, 'Hades');
  assert.equal(filled.release_year, '2019');
  assert.equal(filled.igdb_id, 99);
  assert.deepEqual(filled.platforms, ['PC', 'Switch']);
  assert.equal(filled.notes, 'mine');
  assert.equal(f.genres.length, 2, 'the original state is not mutated');
});

test('year picker options run from this year back to 1990, then the periods', () => {
  const o = yearOptions(2026);
  assert.equal(o[0], '2026');
  assert.equal(o[o.length - 3], '1990');
  assert.deepEqual(o.slice(-2), ['Childhood', 'Always']);
  assert.equal(o.length, 2026 - 1990 + 1 + 2);
});

test('typed year tokens are normalised or rejected', () => {
  assert.equal(normalizeYearToken(' 2024 '), '2024');
  assert.equal(normalizeYearToken('childhood'), 'Childhood');
  assert.equal(normalizeYearToken('ALWAYS'), 'Always');
  for (const bad of ['24', 'later', '20245', '']) assert.equal(normalizeYearToken(bad), null, bad);
});

test('years outside what the API accepts (1970-2100) are rejected before saving', () => {
  assert.equal(normalizeYearToken('1965'), null);
  assert.equal(normalizeYearToken('2101'), null);
  assert.equal(normalizeYearToken('1970'), '1970');
  assert.equal(normalizeYearToken('2100'), '2100');
  const f = formFromGame(game());
  f.years = ['2025', '1965'];
  assert.match(validateForm(f), /between 1970 and 2100/);
  f.years = ['2025', '1970'];
  assert.equal(validateForm(f), '');
  // and a stray out-of-range year never reaches the patch
  f.years = ['2025', '2026', '1965', 'Childhood'];
  assert.deepEqual(buildPatch(game(), f), {});
});
