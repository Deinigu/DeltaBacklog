// Mapping IGDB platform names onto the names the library already uses. Pure (no DOM): unit-tested.
// KEEP IN SYNC with platformAliases / PlatformKey in internal/igdblink/match.go (the IGDB sync and enrichment); a Go test compares them.

// Letters and digits only, lowercase: "Wii U", "WiiU" and "wii-u" are the same platform.
const key = (s) => String(s).toLowerCase().replace(/[^a-z0-9]/g, '');

// IGDB sometimes gives only a long name. Keys are normalised names, values the short names used in the library.
const ALIASES = {
  pcmicrosoftwindows: 'PC', windows: 'PC', microsoftwindows: 'PC',
  nintendoswitch: 'Switch', nintendoswitch2: 'Switch 2', nsw: 'Switch',
  wiiu: 'WiiU', nintendowiiu: 'WiiU', nintendowii: 'Wii',
  nintendods: 'NDS', nintendo3ds: '3DS', new3dsnintendo: '3DS', nintendogamecube: 'NGC', gamecube: 'NGC',
  nintendoentertainmentsystem: 'NES', playstation: 'PS1', ps: 'PS1', playstation2: 'PS2', playstation3: 'PS3', playstation4: 'PS4', playstation5: 'PS5',
  playstationportable: 'PSP', playstationvita: 'Vita', psvita: 'Vita',
  xboxone: 'XONE', xbox360: 'X360', xboxseriesxs: 'Series X|S',
  oculusquest: 'Quest', oculusquest2: 'Quest', metaquest2: 'Quest', metaquest3: 'Quest',
  ios: 'iOS', android: 'Android', mac: 'Mac', macos: 'Mac', linux: 'Linux',
};

/**
 * names: IGDB platform names or abbreviations. library: the platform names already in the library.
 * Returns the matching library-style names (library spelling wins), without duplicates, in input order.
 */
export function mapPlatforms(names, library) {
  const lib = new Map(library.map((n) => [key(n), n]));
  const out = [];
  for (const raw of names) {
    const k = key(raw);
    if (!k) continue;
    const alias = ALIASES[k];
    const name = lib.get(k) ?? (alias ? (lib.get(key(alias)) ?? alias) : String(raw).trim());
    if (!out.includes(name)) out.push(name);
  }
  return out;
}

/**
 * What to pre-select for a looked-up game: of the platforms it is on, the one the library already has the
 * most games on (so Switch beats Switch 2, Wii beats Wii U, and PC wins whenever it is available).
 * Ties keep IGDB's order. Platforms new to the library rank last. counts: Map(platform name -> number of games).
 */
export function defaultPlatforms(mapped, counts = new Map()) {
  if (!mapped.length) return [];
  let best = mapped[0];
  for (const name of mapped) if ((counts.get(name) || 0) > (counts.get(best) || 0)) best = name;
  return [best];
}
