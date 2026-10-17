import { spawnSync } from "node:child_process";
import { createHash } from "node:crypto";
import { readFileSync } from "node:fs";
import { createRequire } from "node:module";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const root = join(dirname(fileURLToPath(import.meta.url)), "..");
const tsc = createRequire(import.meta.url).resolve("typescript/bin/tsc");
const baseline = JSON.parse(
  readFileSync(join(root, "tsc-index-baseline.json"), "utf8"),
);
if (
  baseline.schemaVersion !== 1 ||
  !Array.isArray(baseline.diagnostics) ||
  baseline.errorCount !== baseline.diagnostics.length
) {
  throw new Error("The indexed-access diagnostic inventory is invalid.");
}

const run = spawnSync(
  process.execPath,
  // Incremental state can reorder printed object properties in diagnostics.
  [tsc, "--noEmit", "--pretty", "false", "--noUncheckedIndexedAccess", "--incremental", "false"],
  { cwd: root, encoding: "utf8" },
);
const output = `${run.stdout ?? ""}${run.stderr ?? ""}`;
const count = (output.match(/error TS\d+/g) ?? []).length;

if (run.error || (run.status !== 0 && count === 0)) {
  console.error("tsc did not complete:", run.error ?? output);
  process.exit(1);
}

const sources = new Map();
const diagnostics = Array.from(
  output.matchAll(/^(.+?)\((\d+),(\d+)\): error (TS\d+): (.*)$/gm),
  ([, file, line, column, code, message]) => {
    if (!sources.has(file)) {
      sources.set(file, readFileSync(join(root, file), "utf8").split(/\r?\n/));
    }
    return {
      file,
      line: Number(line),
      column: Number(column),
      code,
      message: message.trim(),
      sourceHash: createHash("sha256")
        .update(sources.get(file)[Number(line) - 1]?.trim() ?? "")
        .digest("hex"),
    };
  },
);
if (diagnostics.length !== count) {
  console.error("Cannot identify every compiler diagnostic:");
  console.error(output);
  process.exit(1);
}

// Line numbers can move without changing an existing diagnostic.
const identity = ({ file, code, message, sourceHash }) =>
  JSON.stringify([file, code, message, sourceHash]);
const remaining = new Map();
for (const diagnostic of baseline.diagnostics) {
  const key = identity(diagnostic);
  remaining.set(key, (remaining.get(key) ?? 0) + 1);
}

const added = [];
for (const diagnostic of diagnostics) {
  const key = identity(diagnostic);
  const available = remaining.get(key) ?? 0;
  if (available > 0) remaining.set(key, available - 1);
  else added.push(diagnostic);
}
const resolved = Array.from(remaining.values()).reduce((sum, value) => sum + value, 0);
if (added.length || resolved) {
  console.error(
    `noUncheckedIndexedAccess: ${count} diagnostics; ${added.length} new, ${resolved} resolved.`,
  );
  for (const diagnostic of added) {
    console.error(
      `${diagnostic.file}(${diagnostic.line},${diagnostic.column}): error ${diagnostic.code}: ${diagnostic.message}`,
    );
  }
  if (resolved) {
    console.error("Remove resolved diagnostics from tsc-index-baseline.json.");
  }
  process.exit(1);
}
console.log(`noUncheckedIndexedAccess: ${count} recorded diagnostics, no new or stale entries.`);
