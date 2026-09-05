// Refuses to build a native bundle that cannot reach the backend.
//
// NEXT_PUBLIC_API_URL is baked in at build time, and the CSP in
// frontend/src/lib/csp.ts names that origin in connect-src. If it is unset or
// unparseable, apiOrigin() returns null and connect-src collapses to 'self':
// the app installs, opens, and then fails every request with no visible
// reason. On a phone the only evidence is a console message nobody is
// watching -- the same silent failure this policy exists to avoid, arriving
// through the policy itself.
//
// Unset is a legitimate configuration for the WEB build (lib/api.ts falls back
// to mock mode), which is why the guard lives here in mobile/ rather than in
// the frontend's own build. An Android app with no backend is not a mode
// anyone wants; it is a mistake.
const raw = (process.env.NEXT_PUBLIC_API_URL ?? "").trim();

const die = (why, hint) => {
  console.error(`\nNEXT_PUBLIC_API_URL ${why}.\n${hint}\n`);
  process.exit(1);
};

if (raw === "") {
  die(
    "is not set, and a native build needs it",
    "Set it to the backend origin, for example:\n" +
      "  NEXT_PUBLIC_API_URL=https://api.example.com npm run sync\n" +
      "In CI this comes from the MOBILE_API_URL secret (.github/workflows/android.yml).",
  );
}

let parsed;
try {
  parsed = new URL(raw);
} catch {
  die(
    `is not a URL: ${JSON.stringify(raw)}`,
    "It must include the scheme, for example https://api.example.com",
  );
}

if (parsed.protocol !== "https:" && parsed.protocol !== "http:") {
  die(
    `must be http or https, not ${JSON.stringify(parsed.protocol)}`,
    "connect-src names the origin by scheme, and anything else cannot be reached from the WebView.",
  );
}

// lib/api.ts composes `${BASE}/workflows`, so a trailing slash produces
// //workflows. Cheap to catch here, and it has bitten this project before.
if (raw.endsWith("/")) {
  die(
    "must not end in a slash",
    `lib/api.ts composes \`\${BASE}/workflows\`, so ${raw} would request ${raw}/workflows.\n` +
      `Use ${raw.replace(/\/+$/, "")}`,
  );
}

console.log(`Building against ${parsed.origin}`);
