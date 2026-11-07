import { describe, expect, it } from "vitest";
import { chainForEnv } from "./chain";

describe("chainForEnv", () => {
  it("defaults blank and whitechain to Whitechain USDT", () => {
    for (const raw of [undefined, "", "  ", "whitechain", "Whitechain"]) {
      const chain = chainForEnv(raw);
      expect(chain.network).toBe("eip155:1874");
      expect(chain.token).toBe("USDT");
    }
  });

  it("selects Arc USDC only for arc", () => {
    const chain = chainForEnv(" ARC ");
    expect(chain.network).toBe("eip155:5042002");
    expect(chain.token).toBe("USDC");
    expect(chain.demoSearchEndpoint).toContain("/api/arc/");
  });

  it("rejects a typo instead of silently showing Whitechain", () => {
    expect(() => chainForEnv("arcc")).toThrow(/not a known rail/);
    expect(() => chainForEnv("ethereum")).toThrow(/not a known rail/);
  });
});
