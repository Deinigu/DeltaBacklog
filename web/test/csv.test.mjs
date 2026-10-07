import test from 'node:test';
import assert from 'node:assert/strict';
import { csvCell, toCSV } from '../static/js/csv.js';

test('csvCell quotes only when needed and doubles quotes', () => {
  assert.equal(csvCell('plain'), 'plain');
  assert.equal(csvCell('a, b'), '"a, b"');
  assert.equal(csvCell('say "hi"'), '"say ""hi"""');
  assert.equal(csvCell('line1\nline2'), '"line1\nline2"');
  assert.equal(csvCell(null), '');
  assert.equal(csvCell(2024), '2024');
});

test('toCSV writes a header, a BOM and CRLF rows', () => {
  const out = toCSV([{ t: 'Hades, II', y: [2025, 2026] }], [{ label: 'Title', text: (g) => g.t }, { label: 'Years', text: (g) => g.y.join('; ') }]);
  assert.equal(out, '\uFEFFTitle,Years\r\n"Hades, II",2025; 2026\r\n');
});

test('cells that look like spreadsheet formulas are defused, plain numbers are not', () => {
  assert.equal(csvCell('=HYPERLINK("http://x")'), `"'=HYPERLINK(""http://x"")"`);
  assert.equal(csvCell('@SUM(A1)'), "'@SUM(A1)");
  assert.equal(csvCell('+1 game'), "'+1 game");
  assert.equal(csvCell('-dash title'), "'-dash title");
  assert.equal(csvCell(-5), '-5');
  assert.equal(csvCell('2024'), '2024');
  assert.equal(csvCell('Half-Life'), 'Half-Life');
});
