// Types for the checkout modal. This is a static-mock UI pass: the shapes are
// intentionally simple and self-contained so mock data can later be swapped for
// real credit line-items / billing state without touching the components.

export type PaymentMethod = "card" | "paypal" | "cod";

export interface CartItem {
  id: string;
  title: string;
  variant: string; // e.g. "Green : M"
  /** Short label rendered inside the placeholder thumbnail tile. */
  thumbLabel: string;
  unitPrice: number; // in whole currency units (USD)
  quantity: number;
}

export interface OrderTotals {
  subtotal: number;
  shipping: number;
  discount: number;
  total: number;
}
