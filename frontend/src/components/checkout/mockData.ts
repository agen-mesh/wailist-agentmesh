import type { CartItem, OrderTotals } from "./types";

// Static mock cart for the credits checkout. AgentMesh users buy USD credit
// top-ups (funded via Razorpay, spent through x402 micropayments), so the cart
// holds a credit bundle rather than physical goods. Isolated here so it can be
// swapped for the real selected top-up later without touching the components.
export const MOCK_CART: CartItem[] = [
  {
    id: "credits-50",
    title: "AgentMesh Credits",
    detail: "$50 top-up · +$5 bonus credits",
    unitPrice: 50,
    quantity: 1,
  },
];

export function computeTotals(items: CartItem[]): OrderTotals {
  const subtotal = items.reduce(
    (sum, item) => sum + item.unitPrice * item.quantity,
    0,
  );
  // Digital credits: no shipping or discount lines — you pay the subtotal.
  return { subtotal, total: subtotal };
}
