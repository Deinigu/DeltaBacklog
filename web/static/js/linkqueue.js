// "Link with IGDB": the review queue's pure logic (no DOM): unit-tested.

/** How trustworthy a title score is, for colouring: high >= 90, mid >= 75, low otherwise. */
export function scoreTone(score) {
  return score >= 90 ? 'high' : score >= 75 ? 'mid' : 'low';
}

/** "Name (year)", or just the name when IGDB has no year. */
export function candidateLabel(c) {
  return c.year ? `${c.name} (${c.year})` : c.name;
}

/** Items not skipped in this visit, in queue order (the server already puts the best proposals first). */
export function remaining(items, skipped) {
  return items.filter((it) => !skipped.has(it.game_id));
}

/** The queue without the game that was just decided. */
export function without(items, gameId) {
  return items.filter((it) => it.game_id !== gameId);
}

/** One line for the header: "12 to review", "12 to review · 3 skipped", or "All linked". */
export function progressText(items, skipped) {
  const left = remaining(items, skipped).length;
  const s = items.length - left;
  if (items.length === 0) return 'All linked';
  if (left === 0) return `${s} skipped for now`;
  return `${left} to review` + (s ? ` · ${s} skipped` : '');
}

/** What a game looks like in the header of its card: "2019 · PC, Switch". */
export function gameMeta(it) {
  return [it.year, it.platforms.join(', ')].filter(Boolean).join(' · ');
}
