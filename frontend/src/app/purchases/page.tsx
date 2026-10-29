import type { Metadata } from "next";
import { PurchasesPage } from "@/components/purchases/PurchasesPage";

export const metadata: Metadata = {
  title: "Service purchases | AgentMesh",
  description: "Discover provider-owned services and approve exact USDC purchases.",
};

export default function Page() {
  return <PurchasesPage />;
}
