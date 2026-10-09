// Time to beat (from IGDB, minutes): which estimate to use, how to show it, and the "short game" rule.
// Pure (no DOM): unit-tested. Games IGDB knows no time for have no estimate and are never counted as short.

/** A game is "short" when its usual time to beat is under this many minutes (10 hours). */
export const SHORT_MINUTES = 600;

const has = (v) => typeof v === 'number' && v > 0;

/** The one number to show and add up: the usual playthrough, else the quick one, else the complete one. null when unknown. */
export function ttbMinutes(g) {
  for (const v of [g.ttb_normally, g.ttb_hastily, g.ttb_completely]) if (has(v)) return v;
  return null;
}

export const isShort = (g) => {
  const m = ttbMinutes(g);
  return m !== null && m < SHORT_MINUTES;
};

/** 45 -> "45 min", 90 -> "1.5 h", 450 -> "7.5 h", 720 -> "12 h", 2500 -> "41 h". Rounds down, so a game under 10 h (isShort) never reads "10 h". */
export function formatTime(minutes) {
  if (!has(minutes)) return '';
  if (minutes < 60) return `${Math.round(minutes)} min`;
  const h = minutes / 60;
  return h < 10 ? `${Math.floor(h * 10) / 10} h` : `${Math.floor(h)} h`;
}

/** [{label: "Quick", text: "8 h"}, {label: "Usual", ...}, ...] for the drawer; parts IGDB does not know are left out. */
export function ttbParts(g) {
  return [['Quick', g.ttb_hastily], ['Usual', g.ttb_normally], ['Complete', g.ttb_completely]]
    .filter(([, v]) => has(v)).map(([label, v]) => ({ label, text: formatTime(v) }));
}

/**
 * Total time of a set of games, for "the backlog is N hours". `known` games have a time, `unknown` have none, so the
 * caller can say how much of the backlog the number covers.
 */
export function totalTime(games) {
  let minutes = 0, known = 0;
  for (const g of games) {
    const m = ttbMinutes(g);
    if (m !== null) { minutes += m; known++; }
  }
  return { minutes, hours: Math.floor(minutes / 60), known, unknown: games.length - known };
}
