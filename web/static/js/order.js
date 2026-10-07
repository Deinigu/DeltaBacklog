// Pure helpers for manual ordering (no DOM, no imports), shared by the data layer and the drag list.

/**
 * Mirrors the server's SlotOrder: the given ids take over the positions they already occupy,
 * in the new order, so games outside the list never move in the global order.
 * Returns Map(id -> position).
 */
export function slotOrder(ids, positions) {
  const slots = ids.map((id) => positions.get(id) ?? 0).sort((a, b) => a - b);
  return new Map(ids.map((id, i) => [id, slots[i]]));
}

export function moveItem(list, from, to) {
  if (from === to || to < 0 || to >= list.length) return list.slice();
  const out = list.slice();
  const [x] = out.splice(from, 1);
  out.splice(to, 0, x);
  return out;
}
