import { describe, it, expect } from "vitest";
import { resourceToNode, formatPrice, type BazaarResource } from "./bazaar";

const base: BazaarResource = {
  id: "r1",
  url: "https://api.example.com/quote",
  method: "GET",
  description: "A quote endpoint",
  merchantId: "m1",
  network: "algorand:wGHE2Pwdvd7S12BL5FaOP20EGYesN73ktiC1qzkkit8=",
  testnet: false,
  amountMicros: 5000,
  asset: "31566704",
  payTo: "PAYTO",
  params: [],
  settleCount: 12,
  host: "api.example.com",
  supported: false,
};

describe("formatPrice", () => {
  it("renders atomic USDC as dollars", () => {
    expect(formatPrice(5000)).toBe("0.005");
    expect(formatPrice(1500000)).toBe("1.5");
    expect(formatPrice(100)).toBe("0.0001");
  });
});

describe("resourceToNode", () => {
  it("builds a tool402 node carrying endpoint, price and method", () => {
    const n = resourceToNode(base);
    expect(n.type).toBe("tool402");
    expect(n.endpoint).toBe("https://api.example.com/quote");
    expect(n.method).toBe("GET");
    expect(n.price).toBe("0.005");
    expect(n.asset).toBe("USDC");
    expect(n.provider).toBe("api.example.com");
  });

  it("marks the node as not-yet-probed so the user still confirms the live price", () => {
    // Catalog data is a mirror, not a live quote — priceLive stays false until
    // the Inspector's Discover button probes the endpoint for real.
    expect(resourceToNode(base).priceLive).toBe(false);
  });

  it("carries declared params through as discoveredParams", () => {
    const n = resourceToNode({
      ...base,
      params: [
        { name: "symbol", type: "string", required: true, description: "example: ALGO" },
      ],
    });
    expect(n.discoveredParams).toHaveLength(1);
    expect(n.discoveredParams?.[0].name).toBe("symbol");
  });

  it("never seeds a param default from a catalog example", () => {
    // The catalog's examples are placeholders (canix publishes the Algorand
    // zero address), so pre-filling them would send junk with real money.
    const n = resourceToNode({
      ...base,
      params: [
        { name: "address", type: "string", required: true, description: "example: AAAA...AAAA" },
      ],
    });
    expect(n.paramDefaults).toEqual({ address: "" });
  });

  it("uses the curated provider name when the entry is supported", () => {
    const n = resourceToNode({ ...base, supported: true, provider: "Tendril" });
    expect(n.provider).toBe("Tendril");
  });

  it("names the node after its supported provider, else its host path", () => {
    expect(resourceToNode({ ...base, supported: true, provider: "Tendril" }).name).toBe(
      "Tendril",
    );
    expect(resourceToNode(base).name).toBe("api.example.com");
  });
});
