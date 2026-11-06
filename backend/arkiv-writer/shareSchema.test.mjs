import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import { buildCreateArgs } from "./shareSchema.mjs";

test("the entity is the payload, for 7 days, readonly", () => {
  const payload = new Uint8Array([9, 8, 7]);
  const args = buildCreateArgs(payload);
  assert.equal(args.schemaName, "agentmesh.share.v1");
  assert.deepEqual(Object.keys(args.values), ["payload"]);
  assert.equal(args.values.payload, payload);
  assert.equal(args.expirationDays, 7);
  assert.equal(args.readonly, true);
  assert.equal(Object.hasOwn(args, "userId"), false);
});

test("the writer script cannot delete or take a user id", () => {
  const source = readFileSync(new URL("./write-share.mjs", import.meta.url), "utf8");
  for (const banned of ["deleteEntity", "patchEntity", "extendEntity", "userId", "NO_SALT"]) {
    assert.equal(source.includes(banned), false, banned);
  }
});
