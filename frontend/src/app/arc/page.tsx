import type { Metadata } from "next";
import { ArcPage } from "@/components/arc/ArcPage";

export const metadata: Metadata = {
  title: "Arc treasury · AgentMesh",
  description:
    "USDC nanopayments, CCTP transfers, and a multichain treasury for AgentMesh on Arc.",
};

export default function Page() {
  return <ArcPage />;
}
