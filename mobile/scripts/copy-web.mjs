// Copies the frontend's static export into www/, which Capacitor serves.
//
// A copy rather than building straight into place: Next 16 refuses a distDir
// that navigates outside its own project ("distDirRoot should not navigate out
// of the projectPath"), so the export lands in frontend/out-mobile and is
// moved here.
import { copyFile, cp, readdir, rm, stat } from "node:fs/promises";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";

const here = dirname(fileURLToPath(import.meta.url));
const src = join(here, "..", "..", "frontend", "out-mobile");
const dest = join(here, "..", "www");

try {
  await stat(src);
} catch {
  console.error(
    `No export at ${src}.\nRun \`npm run build:web\` first (or \`npm run sync\`, which does both).`,
  );
  process.exit(1);
}

// Replace rather than merge: a stale chunk left behind from a previous build
// is served happily by the WebView and produces failures that look like source
// changes having no effect.
await rm(dest, { recursive: true, force: true });
await cp(src, dest, { recursive: true });

// Next's static export writes each page's segment prefetch as nested folders,
// workflows/__next.workflows/__PAGE__.txt, but the client router asks for one
// dotted file, workflows/__next.workflows.__PAGE__.txt. Every navigation in
// the app 404ed on it and fell back to a second, fuller request. A copy under
// the name the router asks for turns that into one request that succeeds.
// Nothing is removed, so a future Next that writes or asks for either form
// still finds it.
async function flattenInto(parent, dir, name) {
  for (const entry of await readdir(dir, { withFileTypes: true })) {
    const path = join(dir, entry.name);
    const flat = `${name}.${entry.name}`;
    if (entry.isDirectory()) await flattenInto(parent, path, flat);
    else await copyFile(path, join(parent, flat));
  }
}
async function flattenSegmentPrefetches(dir) {
  let count = 0;
  for (const entry of await readdir(dir, { withFileTypes: true })) {
    if (!entry.isDirectory()) continue;
    const path = join(dir, entry.name);
    if (entry.name.startsWith("__next.")) {
      await flattenInto(dir, path, entry.name);
      count++;
    } else {
      count += await flattenSegmentPrefetches(path);
    }
  }
  return count;
}
const flattened = await flattenSegmentPrefetches(dest);

console.log(
  `copied ${src} -> ${dest} (${flattened} prefetch folders flattened)`,
);
