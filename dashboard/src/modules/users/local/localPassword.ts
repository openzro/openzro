// Client-side mirror of the ADR-0024 D7 password policy enforced by the
// management server: at least 12 characters (Unicode code points, like
// Go's rune count) and at most 72 bytes of UTF-8, bcrypt's input limit.
// The server stays authoritative; this only spares a round trip.

export const LOCAL_PASSWORD_MIN_CHARS = 12;
export const LOCAL_PASSWORD_MAX_BYTES = 72;

export function localPasswordError(password: string): string | undefined {
  if (password.length === 0) return undefined;
  if (Array.from(password).length < LOCAL_PASSWORD_MIN_CHARS) {
    return `Use at least ${LOCAL_PASSWORD_MIN_CHARS} characters.`;
  }
  if (new TextEncoder().encode(password).length > LOCAL_PASSWORD_MAX_BYTES) {
    return `Use at most ${LOCAL_PASSWORD_MAX_BYTES} bytes.`;
  }
  return undefined;
}

export function isValidLocalPassword(password: string, confirm: string) {
  return (
    password.length > 0 &&
    localPasswordError(password) === undefined &&
    password === confirm
  );
}
