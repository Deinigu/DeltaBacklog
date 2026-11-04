// Keyboard moves in the games table (a roving focus between cells). Pure, so it can be tested without a browser
// (web/test/tablegrid.test.mjs); table.js owns the DOM.

/**
 * Where the focus goes from cell {row, col} on `key`, in a grid of `rows` x `cols`. `page` is how many rows a PageUp /
 * PageDown moves, `ctrl` is the Ctrl (or Cmd) key. Returns the new {row, col}, or null when the key is not a move.
 * The edges stop the focus: no wrapping from one end to the other.
 */
export function nextCell(pos, key, { rows, cols, page = 10, ctrl = false }) {
  if (rows <= 0 || cols <= 0) return null;
  const clamp = (n, max) => Math.min(Math.max(n, 0), max - 1);
  let { row, col } = pos;
  switch (key) {
    case 'ArrowUp': row -= 1; break;
    case 'ArrowDown': row += 1; break;
    case 'ArrowLeft': col -= 1; break;
    case 'ArrowRight': col += 1; break;
    case 'PageUp': row -= page; break;
    case 'PageDown': row += page; break;
    case 'Home': col = 0; if (ctrl) row = 0; break;
    case 'End': col = cols - 1; if (ctrl) row = rows - 1; break;
    default: return null;
  }
  return { row: clamp(row, rows), col: clamp(col, cols) };
}

/** True for the keys that start editing the focused cell. */
export const isEditKey = (key) => key === 'Enter' || key === 'F2';
