import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import type { Workflow } from "@/lib/types";

const device = vi.hoisted(() => ({ compact: true, native: false }));
const api = vi.hoisted(() => ({ list: vi.fn(), get: vi.fn(), setSchedule: vi.fn(), clearSchedule: vi.fn() }));
vi.mock("next/navigation", () => ({ useRouter: () => ({ push: vi.fn() }) }));
vi.mock("@/components/Topbar", () => ({ Topbar: () => null }));
vi.mock("@/components/runs/UpcomingRuns", () => ({ UpcomingRuns: () => null }));
vi.mock("@/hooks/useIsCompact", () => ({ useIsCompact: () => device.compact }));
vi.mock("@/lib/nativeAuth", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/lib/nativeAuth")>()),
  get IS_NATIVE() { return device.native; },
}));
vi.mock("@/lib/api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/lib/api")>()),
  workflows: api,
}));
vi.mock("@/lib/credits/store", () => ({
  useCredits: () => ({ balanceUSD: 0, balanceKnown: true, refreshBalance: vi.fn() }),
}));

import { WorkflowsPage } from "./WorkflowsPage";

const workflow = { id: "scheduled", name: "Scheduled report", status: "deployed", graph: { nodes: [], edges: [] }, updatedAt: "2026-10-01T12:00:00Z" } as unknown as Workflow;

beforeEach(() => {
  device.compact = true;
  device.native = false;
  vi.clearAllMocks();
  api.list.mockResolvedValue([workflow]);
  api.get.mockResolvedValue({ ...workflow, scheduleCron: "*/15 * * * *" });
  api.setSchedule.mockResolvedValue({ cron: "0 9 * * *", nextRunAt: "2026-11-01T09:00:00Z" });
  api.clearSchedule.mockResolvedValue(undefined);
  Object.defineProperty(navigator, "userAgentData", { configurable: true, value: { platform: "Windows", mobile: false } });
  vi.stubGlobal("matchMedia", vi.fn((media: string) => ({ media, matches: false, addEventListener: vi.fn(), removeEventListener: vi.fn() })));
});

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  Reflect.deleteProperty(navigator, "userAgentData");
});

async function openSchedule() {
  render(<WorkflowsPage />);
  fireEvent.click(await screen.findByRole("button", { name: "Workflow actions" }));
  fireEvent.click(screen.getByRole("menuitem", { name: "Schedule" }));
  await screen.findByRole("button", { name: "Remove" });
}

describe("workflow schedule caller", () => {
  it("labels a schedule already hydrated by the list endpoint", async () => {
    api.list.mockResolvedValue([{ ...workflow, scheduleCron: "*/15 * * * *" }]);
    render(<WorkflowsPage />);
    fireEvent.click(await screen.findByRole("button", { name: "Workflow actions" }));
    expect(screen.getByRole("menuitem", { name: "Edit schedule" })).toBeTruthy();
    expect(api.get).not.toHaveBeenCalled();
  });

  it.each(["*/15 * * * *", "0 14 * * 1,5"])("preserves custom %s until replacement is explicitly selected", async (cron) => {
    api.get.mockResolvedValue({ ...workflow, scheduleCron: cron });
    await openSchedule();
    const save = screen.getByRole("button", { name: "Save" });
    expect(save).toHaveProperty("disabled", true);
    fireEvent.click(save);
    expect(api.setSchedule).not.toHaveBeenCalled();
    expect(screen.queryByDisplayValue("09:00")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: /back/i }));
    expect(api.clearSchedule).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("menuitem", { name: "Schedule" }));
    fireEvent.click(await screen.findByRole("button", { name: "Replace schedule" }));
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    await waitFor(() => expect(api.setSchedule).toHaveBeenCalledTimes(1));
  });

  it.each(["save", "remove"])("shows one safe inline message after a failed %s", async (action) => {
    api.get.mockResolvedValue({ ...workflow, scheduleCron: "0 9 * * *" });
    api.setSchedule.mockRejectedValue(new Error("invalid cron 0 9 * * *"));
    api.clearSchedule.mockRejectedValue(new Error("cron storage failure"));
    await openSchedule();
    if (action === "save") {
      fireEvent.click(screen.getByRole("button", { name: "Save" }));
    } else {
      fireEvent.click(screen.getByRole("button", { name: "Remove" }));
      fireEvent.click(screen.getByRole("button", { name: "Remove permanently?" }));
    }
    const message = action === "save" ? "The schedule couldn't be saved. Please try again." : "The schedule couldn't be removed. Please try again.";
    await screen.findByText(message);
    expect(screen.getAllByText(message)).toHaveLength(1);
    expect(document.body.textContent).not.toContain("cron");
  });

  it("keeps schedule editing unavailable on the native handheld policy path", async () => {
    device.native = true;
    Object.defineProperty(navigator, "userAgentData", { configurable: true, value: { platform: "Android", mobile: true } });
    render(<WorkflowsPage />);
    await screen.findByText("Scheduled report");
    expect(screen.queryByRole("button", { name: "Workflow actions" })).toBeNull();
    expect(api.get).not.toHaveBeenCalled();
    expect(api.setSchedule).not.toHaveBeenCalled();
  });
});
