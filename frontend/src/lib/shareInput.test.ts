import { describe, expect, it } from "vitest";
import { classifyShareInput } from "./shareInput";

describe("classifyShareInput", () => {
  it("reads a link from whatever the sender's clipboard actually held", () => {
    // Every one of these is a real shape a link arrives in: with and without
    // a scheme, with www, with a trailing slash, with the tracking query a
    // chat client staples on, and with the whitespace a paste drags along.
    for (const text of [
      "https://www.agent-mesh.app/s/k3Jm9xQpLr2sTvWyZa1b",
      "https://agent-mesh.app/s/k3Jm9xQpLr2sTvWyZa1b",
      "agent-mesh.app/s/k3Jm9xQpLr2sTvWyZa1b",
      "https://www.agent-mesh.app/s/k3Jm9xQpLr2sTvWyZa1b/",
      "https://www.agent-mesh.app/s/k3Jm9xQpLr2sTvWyZa1b?utm_source=chat",
      "https://www.agent-mesh.app/s/k3Jm9xQpLr2sTvWyZa1b#top",
      "  https://www.agent-mesh.app/s/k3Jm9xQpLr2sTvWyZa1b  ",
    ]) {
      expect(classifyShareInput(text)).toEqual({
        kind: "token",
        token: "k3Jm9xQpLr2sTvWyZa1b",
      });
    }
  });

  it("accepts a link from a preview or a local build", () => {
    // Refusing a non-production host would be pointless theatre: the token is
    // looked up against whichever backend this app talks to, whatever host
    // the pasted string happens to name.
    expect(
      classifyShareInput("http://localhost:3000/s/k3Jm9xQpLr2sTvWyZa1b"),
    ).toEqual({ kind: "token", token: "k3Jm9xQpLr2sTvWyZa1b" });
  });

  it("reads a bare token", () => {
    expect(classifyShareInput("k3Jm9xQpLr2sTvWyZa1b")).toEqual({
      kind: "token",
      token: "k3Jm9xQpLr2sTvWyZa1b",
    });
  });

  it("reads a code by its prefix, however long it is", () => {
    const code = "am1." + "H4sIAAAAAAAA".repeat(30);
    expect(classifyShareInput(code)).toEqual({ kind: "code", code });
  });

  it("treats a long base64 blob as a code, not a token", () => {
    // A legacy code has no prefix, so length is the only thing separating it
    // from a token. One node gzipped is already well past 64 characters.
    const legacy =
      "H4sIAAAAAAAAA6tWKkotLsnMS1WyUjI0MjYxNTO3sLSytlGqBQCz".repeat(4);
    expect(classifyShareInput(legacy)).toEqual({ kind: "code", code: legacy });
  });

  it("treats standard-base64 characters as a code even when short", () => {
    // "+" and "/" cannot occur in a token, so their presence settles it.
    expect(classifyShareInput("H4sIAA+AAA/AAA=")).toEqual({
      kind: "code",
      code: "H4sIAA+AAA/AAA=",
    });
  });

  it("finds the link inside whatever was pasted around it", () => {
    // A link almost never arrives alone. It comes with the sentence somebody
    // typed around it, in the angle brackets a mail client adds, or with the
    // sentence's own full stop stuck to the end.
    //
    // The earlier version required the WHOLE paste to be a URL and captured
    // everything to the end of the path, so the first two of these were read
    // as a code and the rest looked up a token with punctuation on it.
    for (const text of [
      "Check this out: https://agent-mesh.app/s/k3Jm9xQpLr2sTvWyZa1b",
      "https://agent-mesh.app/s/k3Jm9xQpLr2sTvWyZa1b is the one I meant",
      "<https://agent-mesh.app/s/k3Jm9xQpLr2sTvWyZa1b>",
      "https://agent-mesh.app/s/k3Jm9xQpLr2sTvWyZa1b.",
      "(https://agent-mesh.app/s/k3Jm9xQpLr2sTvWyZa1b)",
      "here you go —\nhttps://agent-mesh.app/s/k3Jm9xQpLr2sTvWyZa1b\nenjoy",
    ]) {
      expect(classifyShareInput(text)).toEqual({
        kind: "token",
        token: "k3Jm9xQpLr2sTvWyZa1b",
      });
    }
  });

  it("does not mistake a legacy code containing /s/ for a link", () => {
    // Standard base64 -- what a legacy code is -- contains "/", so a long
    // enough one eventually holds the characters "/s/" by chance. Scanning
    // for "/s/" alone would read this as a link and look up a token that
    // never existed, which is why a scheme or a dotted host is required in
    // front of it. Base64 has neither.
    const legacy = "H4sIAAAAAAAAA6tW/s/k3Jm9xQpLr2sTvWyZa1bMLSytlGqBQCz".repeat(
      4,
    );
    expect(classifyShareInput(legacy)).toEqual({ kind: "code", code: legacy });
  });

  it("returns null for nothing at all", () => {
    expect(classifyShareInput("")).toBeNull();
    expect(classifyShareInput("   \n ")).toBeNull();
  });
});
