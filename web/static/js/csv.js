// CSV export of the rows a table shows. Pure (no DOM) so it can be unit-tested.

/** Quote a cell when it contains a comma, quote, newline or leading/trailing space. */
export function csvCell(v) {
  const s = v == null ? '' : String(v);
  return /[",\r\n]|^\s|\s$/.test(s) ? `"${s.replace(/"/g, '""')}"` : s;
}

/** columns: [{label, text(game)}]. Returns the CSV text, with a BOM so Excel reads UTF-8 correctly. */
export function toCSV(games, columns) {
  const lines = [columns.map((c) => csvCell(c.label)).join(',')];
  for (const g of games) lines.push(columns.map((c) => csvCell(c.text(g))).join(','));
  return '\uFEFF' + lines.join('\r\n') + '\r\n';
}
