// Keep the browser wordmark identical to internal/ui/header_actions.go. Decode
// the two terminal half-block rows into a four-row bitmap, independent of fonts.
export const title = 'CODEXOMETER';
const header = [
  '█▀▀ █▀█ █▀▄ █▀▀ ▀▄▀ █▀█ █▀▄▀█ █▀▀ ▀█▀ █▀▀ █▀█',
  '█▄▄ █▄█ █▄▀ ██▄ █ █ █▄█ █ ▀ █ ██▄  █  ██▄ █▀▄',
];
const bitmap = header.flatMap((line) => [
  [...line].map((cell) => cell === '█' || cell === '▀'),
  [...line].map((cell) => cell === '█' || cell === '▄'),
]);
export const logoWidth = bitmap[0].length;
export const logoHeight = bitmap.length;
let column = 0;
const slots = [...title].map((letter) => {
  const width = letter === 'M' ? 5 : 3;
  const slot = { start: column, width };
  column += width + 1;
  return slot;
});
const alphabet = 'ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789';
// Random glyphs share the TUI's tiny font; resolved letters use the header art.
const glyphs = [
  '010 101 111 101',
  '110 111 101 110',
  '111 100 100 111',
  '110 101 101 110',
  '111 100 110 111',
  '111 100 110 100',
  '111 100 101 111',
  '101 101 111 101',
  '111 010 010 111',
  '001 001 101 111',
  '101 110 110 101',
  '100 100 100 111',
  '101 111 101 101',
  '101 111 111 101',
  '111 101 101 111',
  '111 101 111 100',
  '111 101 111 001',
  '111 101 110 101',
  '111 100 001 111',
  '111 010 010 010',
  '101 101 101 111',
  '101 101 101 010',
  '101 101 111 101',
  '101 010 010 101',
  '101 101 010 010',
  '111 001 100 111',
  '111 101 101 111',
  '110 010 010 111',
  '111 001 110 111',
  '111 011 001 111',
  '101 101 111 001',
  '111 110 001 111',
  '100 111 101 111',
  '111 001 010 010',
  '111 111 101 111',
  '111 101 111 001',
].map((glyph) => glyph.split(' '));

export function wordmarkPath(text = title, cursor = -1, dot = false): string {
  let path = '';
  for (let row = 0; row < logoHeight; row++) {
    for (let position = 0; position < slots.length; position++) {
      const slot = slots[position];
      const character = text[position] || ' ';
      const glyph = glyphs[alphabet.indexOf(character)];
      for (let col = 0; col < slot.width; col++) {
        const lit =
          character === title[position]
            ? bitmap[row][slot.start + col]
            : glyph?.[row][Math.floor((col * 3) / slot.width)] === '1';
        const cursorPixel =
          dot &&
          position === cursor &&
          row === logoHeight - 1 &&
          col === Math.floor(slot.width / 2);
        if (lit || cursorPixel) path += `M${slot.start + col} ${row}h1v1h-1z`;
      }
    }
  }
  return path;
}

export type Entrance = 'slide' | 'typing' | 'shuffle';
export const entrances: Entrance[] = ['slide', 'typing', 'shuffle'];
export const entranceDuration = { slide: 900, typing: 2070, shuffle: 1210 };
export const holdDuration = 180;
export const dockDuration = 800;

function randomSource(seed: number): () => number {
  return () => {
    seed |= 0;
    seed = (seed + 0x6d2b79f5) | 0;
    let value = Math.imul(seed ^ (seed >>> 15), 1 | seed);
    value ^= value + Math.imul(value ^ (value >>> 7), 61 | value);
    return ((value ^ (value >>> 14)) >>> 0) / 4294967296;
  };
}

export function createEntrance() {
  const variant = entrances[Math.floor(Math.random() * entrances.length)];
  const seed = Math.floor(Math.random() * 4294967296);
  const random = randomSource(seed);
  const order = [...title].map((_, index) => index);
  for (let index = order.length - 1; index > 0; index--) {
    const other = Math.floor(random() * (index + 1));
    [order[index], order[other]] = [order[other], order[index]];
  }
  return { variant, seed, order };
}

export function lettering(
  state: ReturnType<typeof createEntrance>,
  elapsed: number,
) {
  if (state.variant === 'typing') {
    const blinking = elapsed < 1080;
    const typed = blinking
      ? 0
      : Math.min(Math.floor((elapsed - 1080) / 90) + 1, title.length);
    return {
      text: title.slice(0, typed).padEnd(title.length),
      cursor: typed,
      dot: blinking
        ? Math.floor(elapsed / 180) % 2 === 0
        : typed < title.length,
    };
  }
  if (state.variant === 'shuffle') {
    // Redraws and resizes do not consume randomness or undo resolved letters.
    const random = randomSource(state.seed ^ Math.floor(elapsed / 70));
    const characters = [...title].map((target) => {
      let character;
      do {
        character = alphabet[Math.floor(random() * alphabet.length)];
      } while (character === target);
      return character;
    });
    const locked = Math.min(Math.floor(elapsed / 110), title.length);
    for (const index of state.order.slice(0, locked))
      characters[index] = title[index];
    return { text: characters.join(''), cursor: -1, dot: false };
  }
  return { text: title, cursor: -1, dot: false };
}
