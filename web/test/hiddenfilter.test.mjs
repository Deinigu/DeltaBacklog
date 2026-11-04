import test from 'node:test';
import assert from 'node:assert/strict';
import { groupHidden } from '../static/js/hidden.js';
import { emptyHiddenFilter, hiddenFilterCount, filterHidden, countSource, hiddenStateFromQuery, hiddenStateToQuery } from '../static/js/hiddenfilter.js';

const resp = {
  deals: [{ id: 4, title: 'Deal Game', hidden_at: '' }, { id: 5, title: 'Zelda Deal', hidden_at: '' }],
  similar: [{ igdb_id: 9, title: 'Sim', year: 2010, because: ['Zelda', 'B'], hidden_at: '' }],
  series: [{ collection_id: 7, igdb_id: 101, title: 'Zed II', year: 2005, series: 'Zed Series', hidden_at: '' }],
};
const groups = groupHidden(resp);
const F = (o) => ({ ...emptyHiddenFilter(), ...o });
const titles = (f) => filterHidden(groups, f).flatMap((g) => g.items.map((i) => i.title));

test('no filter keeps every row', () => {
  assert.deepEqual(titles(F({})), ['Deal Game', 'Zelda Deal', 'Sim', 'Zed II']);
});

test('search by title, or by the note (the series, the games it was suggested because of)', () => {
  assert.deepEqual(titles(F({ q: 'zed' })), ['Zed II']);
  assert.deepEqual(titles(F({ q: 'zelda' })), ['Zelda Deal', 'Sim']);
  assert.deepEqual(titles(F({ q: 'ZELDA deal' })), ['Zelda Deal']);
});

test('source: Deals, Similar games, Series', () => {
  assert.deepEqual(titles(F({ sources: new Set(['deals']) })), ['Deal Game', 'Zelda Deal']);
  assert.deepEqual(titles(F({ sources: new Set(['similar', 'series']) })), ['Sim', 'Zed II']);
  assert.deepEqual(titles(F({ sources: new Set(['series']), q: 'zelda' })), []);
});

test('groups left empty are dropped', () => {
  assert.deepEqual(filterHidden(groups, F({ q: 'sim' })).map((g) => g.kind), ['similar']);
});

test('source chip counts follow the search, not the other sources', () => {
  const f = F({ q: 'zelda', sources: new Set(['series']) });
  assert.equal(countSource(groups, f, 'deals'), 1);
  assert.equal(countSource(groups, f, 'similar'), 1);
  assert.equal(countSource(groups, f, 'series'), 0);
  assert.equal(hiddenFilterCount(F({ q: 'x', sources: new Set(['deals', 'series']) })), 2);
});

test('the address round-trips and drops what is unknown', () => {
  const f = F({ q: 'zed', sources: new Set(['series', 'deals']) });
  const qs = hiddenStateToQuery({ filter: f });
  assert.equal(qs, 'q=zed&source=deals%2Cseries');
  assert.deepEqual(hiddenStateFromQuery(qs).filter, f);
  assert.equal(hiddenStateToQuery({ filter: F({}) }), '');
  assert.equal(hiddenStateFromQuery('source=other,deals').filter.sources.size, 1);
});
