import test from 'node:test';
import assert from 'node:assert/strict';
import { mapPlatforms, defaultPlatforms } from '../static/js/platforms.js';

const LIB = ['PC', 'PS3', 'Wii', 'NDS', '3DS', 'Switch', 'PS2', 'WiiU', 'Quest', 'Android', 'PSP', 'iOS', 'Switch 2', 'NGC', 'PS4', 'NES'];

test('IGDB abbreviations already used by the library map to the library spelling', () => {
  assert.deepEqual(mapPlatforms(['PC', 'PS3', 'WiiU', 'NDS'], LIB), ['PC', 'PS3', 'WiiU', 'NDS']);
  assert.deepEqual(mapPlatforms(['Wii U', 'wii-u'], LIB), ['WiiU']); // spelling differences collapse
});

test('long names are mapped through aliases', () => {
  assert.deepEqual(mapPlatforms(['Nintendo Switch', 'PC (Microsoft Windows)', 'Oculus Quest', 'Nintendo Switch 2'], LIB), ['Switch', 'PC', 'Quest', 'Switch 2']);
});

test('platforms the library does not have yet keep a sensible short name', () => {
  assert.deepEqual(mapPlatforms(['PS5', 'XONE', 'Series X|S', 'Mac'], LIB), ['PS5', 'XONE', 'Series X|S', 'Mac']);
  assert.deepEqual(mapPlatforms(['Dreamcast'], LIB), ['Dreamcast']);
  assert.deepEqual(mapPlatforms(['PlayStation 5'], LIB), ['PS5']);
});

test('empty and duplicate names are ignored', () => {
  assert.deepEqual(mapPlatforms(['', 'PC', 'pc', '  '], LIB), ['PC']);
  assert.deepEqual(mapPlatforms([], LIB), []);
});

test('default: the platform the library has the most games on wins', () => {
  const counts = new Map([['PC', 375], ['Switch', 23], ['Switch 2', 3], ['Wii', 38], ['WiiU', 12], ['PS4', 2]]);
  assert.deepEqual(defaultPlatforms(['Switch'], counts), ['Switch']);
  assert.deepEqual(defaultPlatforms(['PS4', 'PC', 'Switch'], counts), ['PC']);
  assert.deepEqual(defaultPlatforms(['Switch 2', 'Switch'], counts), ['Switch']);
  assert.deepEqual(defaultPlatforms(['Wii', 'WiiU'], counts), ['Wii']);
});

test('default: ties keep IGDB order, unknown platforms rank last, nothing gives nothing', () => {
  assert.deepEqual(defaultPlatforms(['PS5', 'XONE']), ['PS5']);
  assert.deepEqual(defaultPlatforms(['PS5', 'Switch'], new Map([['Switch', 1]])), ['Switch']);
  assert.deepEqual(defaultPlatforms([]), []);
});
