/** Letters and digits without the look-alikes (I, l, 1, O, 0) so a password reads out and types cleanly. */
export const PASSWORD_ALPHABET = "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789";
export const GENERATED_PASSWORD_LENGTH = 20;

type FillRandom = (a: Uint32Array) => Uint32Array;

/**
 * A random password drawn uniformly from {@link PASSWORD_ALPHABET} (rejection sampling, so there is no
 * modulo bias). `fill` defaults to `crypto.getRandomValues` and exists so tests can inject a source.
 */
export function generatePassword(length: number = GENERATED_PASSWORD_LENGTH, fill: FillRandom = (a) => crypto.getRandomValues(a)): string {
  const n = PASSWORD_ALPHABET.length;
  const limit = Math.floor(0x100000000 / n) * n; // draws at or above this would bias the low symbols
  let out = "";
  while (out.length < length) {
    const buf = fill(new Uint32Array(length));
    for (const v of buf) {
      if (v >= limit) continue;
      out += PASSWORD_ALPHABET[v % n];
      if (out.length === length) break;
    }
  }
  return out;
}
