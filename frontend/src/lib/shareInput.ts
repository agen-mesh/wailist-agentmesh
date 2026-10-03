// Working out what somebody just pasted into Import.
//
// Three things arrive through one box, because asking a person to classify
// their own clipboard before pasting it is the kind of friction that makes a
// feature go unused:
//
//   a full link   https://www.agent-mesh.app/s/k3Jm9x...
//   a bare token  k3Jm9x...
//   a code        am1.H4sIAAAA... (or a legacy bare-base64 one)
//
// Kept out of the modal so the rules are testable without a DOM, and so the
// awkward case below is written down once rather than re-derived.

export type ShareInput =
  { kind: "token"; token: string } | { kind: "code"; code: string };

// A token is what randURLSafe(16) produces: 22 base64url characters. The range
// is wider than that on purpose -- the backend's CHECK allows 16 to 64, and a
// client hard-coding 22 would start rejecting valid links the day that
// changes.
const TOKEN_SHAPE = /^[A-Za-z0-9_-]{16,64}$/;

const CODE_PREFIX = "am1.";

export function classifyShareInput(raw: string): ShareInput | null {
  const text = raw.trim();
  if (!text) return null;

  // A link, however it was pasted -- with or without a scheme, from any
  // origin, and with whatever query or fragment a chat client stapled on.
  // The path is the only part that matters.
  const fromUrl = tokenFromUrl(text);
  if (fromUrl) return { kind: "token", token: fromUrl };

  // A code is self-identifying since am1., so it never has to be guessed at.
  if (text.startsWith(CODE_PREFIX)) return { kind: "code", code: text };

  // The one genuinely ambiguous case: a bare string of base64url characters
  // is shaped like a token AND like a short legacy code. Tokens are short and
  // legacy codes are not -- a gzipped one-node graph is already a couple of
  // hundred characters -- so length is the tie-breaker, and the caller falls
  // back to trying it as a code if the lookup comes back empty. Guessing
  // wrong costs one failed request; refusing to guess costs the paste.
  if (TOKEN_SHAPE.test(text)) return { kind: "token", token: text };

  return { kind: "code", code: text };
}

// tokenFromUrl pulls the token out of a link, wherever in the paste it sits.
//
// Deliberately origin-agnostic: a link copied from a preview deployment, from
// staging or from localhost is still that person's link, and refusing it
// because the host is not production would be pointless -- the token is
// looked up against THIS backend regardless of what host the string names.
//
// It SCANS rather than parsing the whole input, because a link almost never
// arrives alone. It comes with a sentence wrapped around it ("check this out:
// <link>"), in angle brackets from a mail client, or with the sentence's full
// stop stuck to the end. The earlier version required the ENTIRE paste to be a
// URL and captured everything to the end of the path, so all three of those
// failed -- the first two as "that doesn't look like a workflow code", the
// third as a lookup for a token with a "." on it.
//
// The token's own alphabet ends the match at the first character a token
// cannot contain, so trailing punctuation, a closing bracket or the next word
// all simply stop it. 16-64 mirrors the backend's CHECK, the same bound
// TOKEN_SHAPE uses above.
//
// A scheme or a dotted host is REQUIRED in front, and that is what keeps this
// from eating a code. Standard base64 -- what a legacy code is -- contains
// "/", so a long enough one will eventually hold the characters "/s/" by
// chance, and a bare scan would read it as a link and look up a token that
// never existed. Base64 has no "." and no "://", so demanding a real-looking
// host in front separates the two cleanly. ("am1." does contain a dot, but a
// new-format code is base64URL, which has no "/" at all, so it cannot reach
// the "/s/".)
const LINK_SHAPE =
  /(?:https?:\/\/|(?:^|\s)(?:localhost|[A-Za-z0-9-]+(?:\.[A-Za-z0-9-]+)+)(?::\d+)?)[^\s<>"'`]*?\/s\/([A-Za-z0-9_-]{16,64})/;

function tokenFromUrl(text: string): string | null {
  const match = LINK_SHAPE.exec(text);
  return match ? match[1] : null;
}
