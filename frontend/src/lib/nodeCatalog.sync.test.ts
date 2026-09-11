import { it, expect } from "vitest";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { buildNodeCatalog } from "./nodeCatalog";

// The chat builder (Go) reads the catalog from a committed JSON file, since it
// cannot import TypeScript. This keeps that file honest: it fails whenever the
// palette, the connector tables or the hand-written specs change without the
// JSON being regenerated. Fix with `npm run gen:node-catalog`, which reruns
// this test in write mode.
const TARGET = path.resolve(
  path.dirname(fileURLToPath(import.meta.url)),
  "../../../backend/internal/engine/nodes/nodecatalog.json",
);

it("backend nodecatalog.json matches buildNodeCatalog()", () => {
  const want = JSON.stringify(buildNodeCatalog(), null, 2) + "\n";
  if (process.env.UPDATE_NODE_CATALOG === "1") {
    fs.writeFileSync(TARGET, want);
    return;
  }
  const have = fs.existsSync(TARGET) ? fs.readFileSync(TARGET, "utf8") : "";
  expect(
    have === want,
    "backend/internal/engine/nodes/nodecatalog.json is stale -- run `npm run gen:node-catalog` in frontend/",
  ).toBe(true);
});
