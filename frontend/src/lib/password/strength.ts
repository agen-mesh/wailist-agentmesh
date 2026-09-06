// Advisory strength scoring for the change-password form.
//
// Deliberately not zxcvbn: that is ~400kB for a form the server already guards,
// and this figure is guidance rather than a gate. What it measures is length and
// character variety -- nothing more. It cannot know a password is common,
// breached, or a keyboard walk, so the UI must describe it as exactly that and
// must never call a password "secure".
//
// The only *enforced* rules are the server's 8-character minimum and bcrypt's
// 72-byte maximum (handlers/auth.go). This scoring never implies a stricter
// policy than the backend actually applies.

export const MIN_LENGTH = 8;

// Mirrors handlers/auth.go's maxPasswordBytes, which is bcrypt's own limit.
// Past it the server answers 400, so the form should say so rather than send a
// request it knows will fail.
export const MAX_BYTES = 72;

// Bytes, not characters: bcrypt's limit is on the encoded input, so "é" costs
// two and an emoji four. Using pw.length here would let a 72-character
// passphrase of accented text sail past a check the server then fails -- which
// is the exact confusion this is meant to remove.
export function passwordBytes(pw: string): number {
  return new TextEncoder().encode(pw).length;
}

export type StrengthLabel =
  | "Too short"
  | "Too long"
  | "Basic"
  | "Fair"
  | "Good"
  | "Strong";

export interface Strength {
  /** 0-4, for the segmented meter. 0 renders as empty. */
  score: number;
  label: StrengthLabel;
  /** Which character classes are present, for the caption. */
  classes: number;
  meetsMinimum: boolean;
  /** True past bcrypt's byte limit -- the server will refuse this one. */
  tooLong: boolean;
}

/**
 * Score a password on length and variety.
 *
 * Length leads deliberately. Variety is cheap to fake -- "Pa$$w0rd" hits every
 * class and is still terrible -- so a password under the minimum can never
 * score above "Too short", and hitting every class cannot on its own reach the
 * top band without real length behind it.
 */
export function scorePassword(pw: string): Strength {
  const classes =
    Number(/[a-z]/.test(pw)) +
    Number(/[A-Z]/.test(pw)) +
    Number(/[0-9]/.test(pw)) +
    Number(/[^A-Za-z0-9]/.test(pw));

  const meetsMinimum = pw.length >= MIN_LENGTH;
  const tooLong = passwordBytes(pw) > MAX_BYTES;

  if (!meetsMinimum) {
    return { score: 0, label: "Too short", classes, meetsMinimum, tooLong };
  }
  // Reported as its own state rather than folded into the score: this is an
  // enforced server rule, not advice about strength, and a 100-character
  // passphrase is not weak -- it is simply longer than bcrypt accepts.
  if (tooLong) {
    return { score: 0, label: "Too long", classes, meetsMinimum, tooLong };
  }

  // Length bands, then variety nudges within them. A 20-character passphrase of
  // one class outranks a short string with every class, which is the honest
  // ordering.
  let score = 1;
  if (pw.length >= 12) score = 2;
  if (pw.length >= 16) score = 3;
  if (pw.length >= 20) score = 4;

  if (classes >= 3 && score < 4) score += 1;
  if (classes === 1 && score > 1) score -= 1;

  const label: StrengthLabel =
    score >= 4
      ? "Strong"
      : score === 3
        ? "Good"
        : score === 2
          ? "Fair"
          : "Basic";

  return { score, label, classes, meetsMinimum, tooLong };
}
