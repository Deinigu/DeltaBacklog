// The "Update details from IGDB?" prompt after a game is linked to another IGDB game: its pure text helpers (unit-tested).

/** "Genres: Action → Indie, Roguelike", with "(empty)" for a field that had no value. */
export function changeText(c) {
  return `${c.label}: ${c.old || '(empty)'} → ${c.new || '(empty)'}`;
}

/** The toast after applying: "Updated 3 fields", "Filled 1 field" or the reason nothing changed. */
export function appliedText(changes, mode) {
  const n = changes.length;
  if (n === 0) return mode === 'replace' ? 'IGDB has nothing different to put in' : 'Nothing to fill: every field already has a value';
  return `${mode === 'replace' ? 'Updated' : 'Filled'} ${n} ${n === 1 ? 'field' : 'fields'} from IGDB`;
}

/** What went wrong, in words for the person (the server's status decides). */
export function errorText(e) {
  switch (e?.status) {
    case 503: return 'IGDB is not set up on this server, so details cannot be read';
    case 502: return 'IGDB did not answer. Nothing was changed; try again later';
    case 409: return 'This game is not linked to that IGDB game any more';
    case 422: return 'IGDB has no details for this game';
    default: return 'Could not update the details. Check the connection and try again';
  }
}

/** A patch that undoes an applied update is worth offering only when something it restores changed. */
export const hasUndo = (undo) => !!undo && Object.keys(undo).length > 0;
