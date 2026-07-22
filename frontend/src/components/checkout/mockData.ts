import type { CartItem, OrderTotals } from "./types";

// Static mock cart mirroring the reference design. Kept isolated so it can be
// swapped for real line-items (e.g. credit top-up packages) later without
// touching the checkout components.
export const MOCK_CART: CartItem[] = [
  {
    id: "mbp-14-m2",
    title: 'MacBook Pro 14" M2 Pro',
    variant: "Green : M",
    thumbLabel: "MBP",
    unitPrice: 105,
    quantity: 1,
  },
  {
    id: "iphone-16-pro-max",
    title: "iPhone 16 Pro Max 256GB",
    variant: "Green : M",
    thumbLabel: "16PM",
    unitPrice: 120,
    quantity: 1,
  },
  {
    id: "magic-keyboard-ipad-11",
    title: 'Magic Keyboard for iPad Pro 11"',
    variant: "Green : M",
    thumbLabel: "KB",
    unitPrice: 199,
    quantity: 1,
  },
];

// Flat mock shipping/discount so the summary matches the reference figures.
export const MOCK_SHIPPING = 599;
export const MOCK_DISCOUNT = 50;

export function computeTotals(items: CartItem[]): OrderTotals {
  const subtotal = items.reduce(
    (sum, item) => sum + item.unitPrice * item.quantity,
    0,
  );
  const shipping = items.length > 0 ? MOCK_SHIPPING : 0;
  const discount = items.length > 0 ? MOCK_DISCOUNT : 0;
  const total = Math.max(0, subtotal + shipping - discount);
  return { subtotal, shipping, discount, total };
}
