import { afterEach, describe, expect, it, vi } from "vitest";
import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
} from "@testing-library/react";

// The gesture as the screen shows it. The maths is in PullToRefresh.test.ts;
// what matters here is that one ring fills while pulling and then spins, and
// that the spin is never put on the bubble that carries the inline transform.
vi.mock("@/hooks/useIsHandheld", () => ({ useIsHandheld: () => true }));
vi.mock("@/native/haptics", () => ({ tapFeedback: vi.fn(async () => {}) }));

import { PULL_THRESHOLD_PX, PullToRefresh } from "./PullToRefresh";

afterEach(cleanup);

const ring = () => screen.queryByTestId("ptr-ring");
const bubble = () => document.querySelector(".ptr-indicator");
// How much of the circle is drawn: dasharray minus dashoffset.
function drawn() {
  const circle = ring()!.querySelector("circle")!;
  const total = Number(circle.getAttribute("stroke-dasharray"));
  return total - Number(circle.getAttribute("stroke-dashoffset"));
}

function pull(surface: HTMLElement, dy: number) {
  fireEvent.touchStart(surface, { touches: [{ clientY: 0 }] });
  fireEvent.touchMove(surface, { touches: [{ clientY: dy }] });
}

function setup(onRefresh = vi.fn(async () => {})) {
  const { container } = render(
    <PullToRefresh onRefresh={onRefresh}>
      <p>rows</p>
    </PullToRefresh>,
  );
  // The scrolling child of the wrapper: the element the gesture listens on.
  const surface = container.firstElementChild!.lastElementChild as HTMLElement;
  return { surface, onRefresh };
}

describe("the pull indicator", () => {
  it("draws nothing until the pull starts", () => {
    setup();
    expect(ring()).toBeNull();
  });

  it("fills the ring as the pull grows, and completes it once armed", () => {
    const { surface } = setup();
    pull(surface, 20);
    const early = drawn();
    expect(early).toBeGreaterThan(0);

    pull(surface, 400);
    expect(drawn()).toBeGreaterThan(early);
    // Armed: the ring is closed, and the whole circle is drawn.
    const circle = ring()!.querySelector("circle")!;
    expect(drawn()).toBeCloseTo(
      Number(circle.getAttribute("stroke-dasharray")),
    );
  });

  it("never rotates anything while the finger is down", () => {
    const { surface } = setup();
    pull(surface, 60);
    expect(ring()!.getAttribute("data-spinning")).toBeNull();
    expect(ring()!.style.transform).toBe("");
    expect(bubble()!.getAttribute("data-spinning")).toBeNull();
  });

  it("spins the ring, not the bubble, while refreshing", async () => {
    let finish!: () => void;
    const onRefresh = vi.fn(
      () =>
        new Promise<void>((r) => {
          finish = r;
        }),
    );
    const { surface } = setup(onRefresh);
    pull(surface, 400);
    await act(async () => {
      fireEvent.touchEnd(surface);
    });

    expect(onRefresh).toHaveBeenCalled();
    expect(ring()!.getAttribute("data-spinning")).toBe("");
    // The bubble keeps its own inline transform, which is why the animation
    // must not be on it: CSS would rotate that translate and send it orbiting.
    expect((bubble() as HTMLElement).style.transform).toContain("translate(");
    expect((bubble() as HTMLElement).style.animation).toBe("");

    await act(async () => {
      finish();
    });
    expect(ring()).toBeNull();
  });

  it("keeps the ring at the resting position while it refreshes", async () => {
    let finish!: () => void;
    const { surface } = setup(
      vi.fn(
        () =>
          new Promise<void>((r) => {
            finish = r;
          }),
      ),
    );
    pull(surface, 400);
    const pulled = (bubble() as HTMLElement).style.transform;
    await act(async () => {
      fireEvent.touchEnd(surface);
    });
    const resting = (bubble() as HTMLElement).style.transform;
    expect(resting).not.toBe(pulled);
    expect(resting).toContain("translate(-50%,");
    await act(async () => {
      finish();
    });
    expect(PULL_THRESHOLD_PX).toBeGreaterThan(0);
  });
});
