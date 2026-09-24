import { afterEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, render, screen, waitFor } from "@testing-library/react";
import { buildUsage } from "@/lib/data";

const state = vi.hoisted(() => ({
  readOnly: false,
  // Held open by a test to keep the reload in flight.
  gate: Promise.resolve() as Promise<void>,
  // Set by a test to make one of the five requests fail at once.
  failTimeseries: false,
  phone: null as null | { onRetry: () => void | Promise<void> },
}));

vi.mock("@/hooks/useReadOnly", () => ({ useReadOnly: () => state.readOnly }));
vi.mock("next/navigation", () => ({ useRouter: () => ({ push: vi.fn() }) }));
vi.mock("@/components/Topbar", () => ({ Topbar: () => null }));
vi.mock("./phone/UsagePhonePage", () => ({
  UsagePhonePage: (p: { onRetry: () => void | Promise<void> }) => {
    state.phone = p;
    return <div>phone usage</div>;
  },
}));
vi.mock("@/lib/api", () => {
  const u = buildUsage("30d");
  return {
    usage: {
      invalidate: () => {},
      summary: async () => {
        await state.gate;
        return u.summary;
      },
      timeseries: async () => {
        if (state.failTimeseries) throw new Error("timeseries down");
        return u.timeseries;
      },
      byWorkflow: async () => u.byWorkflow,
      byEndpoint: async () => u.byEndpoint,
      settlements: async () => u.settlements,
    },
  };
});

import { UsagePage } from "./UsagePage";

afterEach(() => {
  cleanup();
  state.readOnly = false;
  state.gate = Promise.resolve();
  state.failTimeseries = false;
  state.phone = null;
});

// The desktop page is two wide tables that only scroll sideways on a phone,
// so a phone -- and the Android app -- gets its own screen.
describe("UsagePage", () => {
  it("gives a phone its own screen", () => {
    state.readOnly = true;
    render(<UsagePage />);
    expect(screen.getByText("phone usage")).toBeTruthy();
  });

  // Pull to refresh stops spinning when the promise it is given settles, so
  // the reload has to hand back one that waits for the requests.
  it("hands the pull a reload that settles when the data has landed", async () => {
    state.readOnly = true;
    render(<UsagePage />);
    await waitFor(() => expect(state.phone).not.toBeNull());

    let open!: () => void;
    state.gate = new Promise<void>((r) => {
      open = r;
    });
    let settled = false;
    let reload!: Promise<void> | void;
    act(() => {
      reload = state.phone!.onRetry();
    });
    void Promise.resolve(reload).then(() => {
      settled = true;
    });
    await act(async () => {
      await new Promise((r) => setTimeout(r, 20));
    });
    expect(settled).toBe(false);

    await act(async () => {
      open();
      await reload;
    });
    expect(settled).toBe(true);
  });

  // Promise.all gives up at the first failure, while the other requests are
  // still out. The pull used to stop there, and the page then changed again
  // under a finished refresh as they landed.
  it("keeps the pull going until every request has answered, even after one fails", async () => {
    vi.spyOn(console, "error").mockImplementation(() => {});
    state.readOnly = true;
    render(<UsagePage />);
    await waitFor(() => expect(state.phone).not.toBeNull());

    let open!: () => void;
    state.gate = new Promise<void>((r) => {
      open = r;
    });
    state.failTimeseries = true;
    let settled = false;
    let reload!: Promise<void> | void;
    act(() => {
      reload = state.phone!.onRetry();
    });
    void Promise.resolve(reload).then(() => {
      settled = true;
    });
    await act(async () => {
      await new Promise((r) => setTimeout(r, 20));
    });
    expect(settled).toBe(false);

    await act(async () => {
      open();
      await reload;
    });
    expect(settled).toBe(true);
  });
});
