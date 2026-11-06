// @vitest-environment node
import { expect, it } from "vitest";
import { openShare, sealShare } from "./shareCrypto";

it("round-trips and keeps the key out of the ciphertext", async () => {
  const plain = new TextEncoder().encode("graph-bytes");
  const sealed = await sealShare(plain);
  expect(sealed.key).toMatch(/^[A-Za-z0-9_-]+$/);
  expect(sealed.key).not.toContain("=");
  expect(new TextDecoder().decode(sealed.ciphertext)).not.toContain("graph-bytes");
  expect(sealed.ciphertext.byteLength).toBe(12 + plain.byteLength + 16);
  const opened = await openShare(sealed.ciphertext, sealed.key);
  expect(Buffer.from(opened).equals(Buffer.from(plain))).toBe(true);
});

it("rejects a wrong key and an empty key with the fragment message", async () => {
  const sealed = await sealShare(new Uint8Array([1, 2, 3, 4]));
  await expect(openShare(sealed.ciphertext, "AAAA")).rejects.toThrow(
    "This private share needs the #k= key from the original link.",
  );
  await expect(openShare(sealed.ciphertext, "")).rejects.toThrow(
    "This private share needs the #k= key from the original link.",
  );
});
