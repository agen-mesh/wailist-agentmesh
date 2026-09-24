"use client";
import { Pill } from "@/components/ui";
import type { RunStatus } from "@/lib/types";

const STATUS: Record<
  RunStatus,
  { label: string; tone: "ok" | "warm" | "danger" | "default" }
> = {
  running: { label: "Running", tone: "warm" },
  success: { label: "Succeeded", tone: "ok" },
  failed: { label: "Failed", tone: "danger" },
  stopped: { label: "Stopped", tone: "default" },
};

// A run's status, spelled out rather than left to colour, so it reads the same
// to someone who cannot tell the tones apart. Only a running run shows a dot.
export function RunStatusPill({ status }: { status: string }) {
  const known = STATUS[status as RunStatus];
  return (
    <Pill mono tone={known?.tone ?? "default"} dot={status === "running"}>
      {known?.label ?? status}
    </Pill>
  );
}
