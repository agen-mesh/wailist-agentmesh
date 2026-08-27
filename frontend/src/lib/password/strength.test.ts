import { describe, expect, it } from "vitest";
import { MIN_LENGTH, scorePassword } from "@/lib/password/strength";

describe("the enforced minimum", () => {
  it("matches what the server enforces", () => {
    // handlers/auth.go rejects under 8. If these drift apart the form either
    // blocks something the server would accept, or invites a request it refuses.
    expect(MIN_LENGTH).toBe(8);
  });

  it("scores anything shorter as Too short, whatever it contains", () => {
    // The interesting case: every character class present, still too short.
    const s = scorePassword("aA1!aA1");
    expect(s.meetsMinimum).toBe(false);
    expect(s.score).toBe(0);
    expect(s.label).toBe("Too short");
    expect(s.classes).toBe(4);
  });

  it("accepts exactly the minimum", () => {
    expect(scorePassword("abcdefgh").meetsMinimum).toBe(true);
  });
});

describe("honesty of the ranking", () => {
  // The whole reason this exists rather than a variety-only score: a short
  // string with every class must not outrank a genuinely long passphrase.
  it("ranks a long passphrase above a short everything-goes password", () => {
    const passphrase = scorePassword("correct horse battery staple");
    const kitchenSink = scorePassword("Pa$$w0rd");
    expect(passphrase.score).toBeGreaterThan(kitchenSink.score);
  });

  it("never labels a minimum-length password Strong", () => {
    expect(scorePassword("aA1!aA1!").label).not.toBe("Strong");
  });

  it("rewards length monotonically for the same character mix", () => {
    const short = scorePassword("abcdefgh");
    const mid = scorePassword("abcdefghijkl");
    const long = scorePassword("abcdefghijklmnopqrst");
    expect(mid.score).toBeGreaterThanOrEqual(short.score);
    expect(long.score).toBeGreaterThanOrEqual(mid.score);
  });
});

describe("character classes", () => {
  it("counts each class once", () => {
    expect(scorePassword("abcdefgh").classes).toBe(1);
    expect(scorePassword("abcdEFGH").classes).toBe(2);
    expect(scorePassword("abcdEF12").classes).toBe(3);
    expect(scorePassword("abcdEF1!").classes).toBe(4);
  });

  it("treats an empty password as too short rather than throwing", () => {
    const s = scorePassword("");
    expect(s.score).toBe(0);
    expect(s.classes).toBe(0);
    expect(s.meetsMinimum).toBe(false);
  });
});

describe("the score stays in range", () => {
  it("never leaves 0-4 for any input", () => {
    const samples = [
      "",
      "a",
      "abcdefgh",
      "aA1!aA1!",
      "x".repeat(64),
      "Corr3ct-Horse-Battery-Staple!",
      "        ",
      "🔥🔥🔥🔥🔥🔥🔥🔥",
    ];
    for (const pw of samples) {
      const { score } = scorePassword(pw);
      expect(score).toBeGreaterThanOrEqual(0);
      expect(score).toBeLessThanOrEqual(4);
    }
  });
});
