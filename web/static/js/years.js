// What a card shows for "years played": the most recent years as chips, a count of older ones, and the named periods.
// Pure (no DOM): unit-tested.

export const PERIOD_ICONS = { childhood: '🧒', always: '♾️' };
export const PERIOD_LABELS = { childhood: 'Childhood', always: 'Always played' };

/**
 * game: {years: number[] ascending, periods: string[]}. Returns
 *  { years: [{year}], earlier: number, periods: [{key, icon, label}] }
 * `years` holds at most `maxYears` of the most recent years, oldest first, so the row reads as a timeline
 * ending at the latest year. `earlier` counts the older years that did not fit.
 */
export function playedBadges(game, maxYears = 3) {
  const all = [...new Set(game.years)].sort((a, b) => a - b);
  const shown = all.slice(-maxYears);
  return {
    years: shown.map((year) => ({ year })),
    earlier: all.length - shown.length,
    periods: game.periods.filter((p) => PERIOD_ICONS[p]).map((key) => ({ key, icon: PERIOD_ICONS[key], label: PERIOD_LABELS[key] })),
  };
}

/** One sentence for tooltips and screen readers: "Played in 2019, 2024, 2025 and as a child". */
export function playedSummary(game) {
  const years = [...new Set(game.years)].sort((a, b) => a - b);
  const parts = [];
  if (years.length) parts.push(`in ${years.join(', ')}`);
  if (game.periods.includes('childhood')) parts.push('as a child');
  if (game.periods.includes('always')) parts.push('on and off');
  return parts.length ? `Played ${parts.join(' and ')}` : 'Not played yet';
}
