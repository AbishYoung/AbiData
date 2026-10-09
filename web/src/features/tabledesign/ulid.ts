const ALPHABET = "0123456789ABCDEFGHJKMNPQRSTVWXYZ";
const TIME_CHARS = 10;
const RANDOM_CHARS = 16;

export const ULID_PATTERN = /^[0-9A-HJKMNP-TV-Z]{26}$/;

/** Encodes a millisecond timestamp as the 10-character Crockford base-32 ULID time prefix. */
export function encodeTime(ms: number): string {
  let rest = Math.floor(ms);
  let out = "";
  for (let i = 0; i < TIME_CHARS; i++) {
    out = ALPHABET.charAt(rest % 32) + out;
    rest = Math.floor(rest / 32);
  }
  return out;
}

/** A 26-character ULID (48-bit time + 80 random bits), uppercase Crockford base-32. */
export function ulid(now: number = Date.now()): string {
  const bytes = new Uint8Array(RANDOM_CHARS);
  crypto.getRandomValues(bytes);
  let random = "";
  for (const b of bytes) random += ALPHABET.charAt(b % 32);
  return encodeTime(now) + random;
}
