// Counts TypeScript errors under noUncheckedIndexedAccess without turning the
// flag on, and fails if the count rises above tsc-index-baseline.json.
//
// #198 was a record lookup with no entry for its key, which this flag reports
// as possibly undefined. Enabling it outright reports 124 existing errors on
// master at 40b6062. The ratchet stops that number growing while they are
// fixed; once it reaches zero the flag can move into tsconfig.json.
import { spawnSync } from "node:child_process";
import { readFileSync } from "node:fs";
import { createRequire } from "node:module";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const root = join(dirname(fileURLToPath(import.meta.url)), "..");
const tsc = createRequire(import.meta.url).resolve("typescript/bin/tsc");
const baseline = JSON.parse(
  readFileSync(join(root, "tsc-index-baseline.json"), "utf8"),
).errors;

const run = spawnSync(
  process.execPath,
  [tsc, "--noEmit", "--pretty", "false", "--noUncheckedIndexedAccess"],
  { cwd: root, encoding: "utf8" },
);
const output = `${run.stdout ?? ""}${run.stderr ?? ""}`;
const count = (output.match(/error TS\d+/g) ?? []).length;

// A tsc that failed to start prints no "error TS" lines, which would otherwise
// read as zero errors and pass.
if (run.error || (run.status !== 0 && count === 0)) {
  console.error("tsc did not complete:", run.error ?? output);
  process.exit(1);
}

if (count > baseline) {
  console.error(
    `noUncheckedIndexedAccess errors: ${count}, above the baseline of ${baseline}.`,
  );
  console.error(
    "A change indexes a record or array without handling a missing entry.",
  );
  console.error(output);
  process.exit(1);
}

if (count < baseline) {
  console.log(
    `noUncheckedIndexedAccess errors: ${count}, below the baseline of ${baseline}. ` +
      `Set "errors" in tsc-index-baseline.json to ${count} to keep the improvement.`,
  );
} else {
  console.log(
    `noUncheckedIndexedAccess errors: ${count} (baseline ${baseline}).`,
  );
}
