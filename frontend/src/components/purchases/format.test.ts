import { describe, expect, it } from "vitest";
import { assertAuthorizationMatches, offerUnavailable, safeOrderId, toMicros, usdc } from "./format";
import { authorization, offer, order, PAYER, TEST_NOW } from "./test-fixtures";

describe("exact purchase terms", () => {
  it("keeps large USDC values exact and rejects fractional micro-units", () => {
    expect(toMicros("9007199254740993.123456")).toBe("9007199254740993123456");
    expect(usdc("9007199254740993123456")).toBe("9007199254740993.123456");
    for (const value of ["0.0000001", "1e3", "-1", "NaN", "01", "1.2.3"]) expect(() => toMicros(value)).toThrow();
  });

  it("checks block expiry separately from the UTC offer deadline", () => {
    expect(offerUnavailable(offer(), TEST_NOW)).toBeNull();
    expect(offerUnavailable(offer({ observedBlock: "500" }), TEST_NOW)).toBe("Expired on Arkiv");
    expect(offerUnavailable(offer({ validUntil: "2026-11-05T11:59:59Z" }), TEST_NOW)).toBe("Offer deadline passed");
    expect(offerUnavailable(offer({ observedBlock: "unknown" }), TEST_NOW)).toBe("Expiry could not be checked");
  });

  it("accepts the exact prepared authorization even when field-object key order differs", () => {
    const prepared = authorization();
    prepared.typedData.types.TransferWithAuthorization = prepared.typedData.types.TransferWithAuthorization.map(({ name, type }) => ({ type, name }));
    prepared.typedData.types.EIP712Domain = prepared.typedData.types.EIP712Domain.map(({ name, type }) => ({ type, name }));
    expect(() => assertAuthorizationMatches(order(), prepared, PAYER, TEST_NOW)).not.toThrow();
  });

  it.each(["name", "version", "chainId", "verifyingContract"])("rejects a domain schema that omits %s", (field) => {
    const prepared = authorization();
    prepared.typedData.types.EIP712Domain = prepared.typedData.types.EIP712Domain.filter(({ name }) => name !== field);
    expect(() => assertAuthorizationMatches(order(), prepared, PAYER, TEST_NOW)).toThrow();
  });

  it("rejects a missing, mistyped or reordered domain schema", () => {
    const missing = authorization(); delete missing.typedData.types.EIP712Domain;
    const mistyped = authorization(); mistyped.typedData.types.EIP712Domain[2].type = "uint128";
    const reordered = authorization(); reordered.typedData.types.EIP712Domain.reverse();
    for (const prepared of [missing, mistyped, reordered]) expect(() => assertAuthorizationMatches(order(), prepared, PAYER, TEST_NOW)).toThrow();
  });

  it.each(["to", "from", "value", "validBefore", "validAfter", "nonce"] as const)("rejects a changed authorization %s", (field) => {
    const prepared = authorization();
    prepared.typedData.message[field] = field === "from" || field === "to" ? `0x${"9".repeat(40)}` : field === "nonce" ? `0x${"9".repeat(64)}` : "9999999999";
    expect(() => assertAuthorizationMatches(order(), prepared, PAYER, TEST_NOW)).toThrow();
  });

  it("rejects a changed network, token, schema, stored offer snapshot or connected payer", () => {
    const wrongChain = authorization(); wrongChain.typedData.domain.chainId = 1;
    const wrongToken = authorization(); wrongToken.typedData.domain.verifyingContract = PAYER;
    const wrongSchema = authorization(); wrongSchema.typedData.types.TransferWithAuthorization[2].type = "uint128";
    const wrongSnapshot = authorization(); wrongSnapshot.order.offer.snapshotHash = "different";
    for (const prepared of [wrongChain, wrongToken, wrongSchema, wrongSnapshot]) expect(() => assertAuthorizationMatches(order(), prepared, PAYER, TEST_NOW)).toThrow();
    expect(() => assertAuthorizationMatches(order(), authorization(), `0x${"3".repeat(40)}`, TEST_NOW)).toThrow();
  });

  it("cannot sign an unapproved, expired or unreserved order", () => {
    expect(() => assertAuthorizationMatches(order({ canAuthorize: false }), authorization(), PAYER, TEST_NOW)).toThrow();
    expect(() => assertAuthorizationMatches(order(), authorization(), PAYER, Date.parse(order().deadline))).toThrow();
    const unreserved = authorization(); delete unreserved.order.signatureReleasedAt;
    expect(() => assertAuthorizationMatches(order(), unreserved, PAYER, TEST_NOW)).toThrow();
  });

  it("stores opaque order IDs without accepting path or query injection", () => {
    expect(safeOrderId("order-1:retry_2")).toBe("order-1:retry_2");
    for (const value of ["../other", "id?approved=true", "id\nother", "", "x".repeat(257)]) expect(safeOrderId(value)).toBeNull();
  });
});
