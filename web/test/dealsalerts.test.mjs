import test from 'node:test';
import assert from 'node:assert/strict';
import { buildDeals, alertOf, unseenCount, isMuted, ALERT_LABEL } from '../static/js/deals.js';

const games = [1, 2, 3].map((id) => ({ id, title: `G${id}`, status: 'backlog' }));
const prices = games.map((g) => ({ game_id: g.id, currency: 'EUR', shop: 'Steam', price: 500, regular: 1000, cut: 50, url: null, low_price: 400, fetched_at: '2026-10-01T00:00:00Z' }));
const info = {
  watched: [1, 2, 3], prices,
  matches: games.map((g) => ({ game_id: g.id, itad_id: `i${g.id}`, source: 'steam' })),
  alerts: [{ game_id: 2, kind: 'two_year', price: 500, at: '2026-10-02T00:00:00Z', seen: false }, { game_id: 3, kind: 'historical', price: 500, at: '2026-10-01T00:00:00Z', seen: true }],
  muted: [1],
};

test('alerts: lookup, unseen count and mute state', () => {
  assert.equal(alertOf(info, 2).kind, 'two_year');
  assert.equal(alertOf(info, 1), null);
  assert.equal(alertOf(null, 1), null);
  assert.equal(unseenCount(info), 1);
  assert.equal(unseenCount({}), 0);
  assert.equal(isMuted(info, 1), true);
  assert.equal(isMuted(info, 2), false);
  assert.equal(isMuted(undefined, 2), false);
  assert.equal(ALERT_LABEL.historical, 'Historical low');
  assert.equal(ALERT_LABEL.two_year, 'Lowest in 2 years');
});

test('the Alerts filter keeps only the games with a standing alert', () => {
  const all = buildDeals(games, info);
  assert.equal(all.rows.length, 3);
  assert.deepEqual(all.rows.find((r) => r.game.id === 2).alert.kind, 'two_year');
  assert.equal(all.rows.find((r) => r.game.id === 1).alert, null);
  const only = buildDeals(games, info, { alertsOnly: true });
  assert.deepEqual(only.rows.map((r) => r.game.id).sort(), [2, 3]);
  assert.equal(only.unmatched.length + only.waiting.length, 0);
  // No alerts at all: nothing, and no crash on an old server's answer.
  assert.equal(buildDeals(games, { ...info, alerts: undefined }, { alertsOnly: true }).rows.length, 0);
});
