import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";

// Play's background-location policy: the prominent disclosure must be on
// screen BEFORE Android's permission dialog, and the dialog must only follow a
// tap on it. DISCLOSURE was written for that and then never rendered -- the
// "Turn on location access" button went straight to the system dialog. These
// tests drive the screen the way a reviewer's video does and pin the order.

const calls = { request: 0, setGeofence: 0, settings: 0 };
let armedAfterGrant = true;

vi.mock("@/lib/nativeAuth", () => ({ IS_NATIVE: true }));
vi.mock("@/hooks/useReadOnly", () => ({ useReadOnly: () => false }));
vi.mock("@/lib/api", () => ({
  workflows: {
    get: async () => ({ id: "wf1", name: "Demo", status: "deployed" }),
  },
}));
vi.mock("@/native", () => ({
  shell: {
    async setGeofence() {
      calls.setGeofence += 1;
      // Unarmed until background location has been granted.
      return calls.request > 0 && armedAfterGrant;
    },
  },
}));
vi.mock("@/native/permissions", () => ({
  DISCLOSURE: {
    title: "Run workflows when you arrive or leave",
    body: "First paragraph.\n\nSecond paragraph.",
    grant: "Choose location access",
    decline: "Not now",
  },
  requestBackgroundLocation: async () => {
    calls.request += 1;
    return "granted";
  },
  openSettings: async () => {
    calls.settings += 1;
  },
}));

let GeofenceScreen: typeof import("./GeofenceScreen").GeofenceScreen;

beforeEach(async () => {
  calls.request = 0;
  calls.setGeofence = 0;
  calls.settings = 0;
  armedAfterGrant = true;
  Object.defineProperty(navigator, "geolocation", {
    configurable: true,
    value: {
      getCurrentPosition: (ok: PositionCallback) =>
        ok({
          coords: { latitude: 22.57, longitude: 88.36, accuracy: 10 },
        } as GeolocationPosition),
    },
  });
  ({ GeofenceScreen } = await import("./GeofenceScreen"));
});

afterEach(cleanup);

async function saveZone() {
  render(<GeofenceScreen workflowId="wf1" />);
  fireEvent.click(
    await screen.findByRole("button", { name: "Use my location" }),
  );
  fireEvent.click(await screen.findByRole("button", { name: "Save zone" }));
}

describe("GeofenceScreen background-location disclosure", () => {
  it("shows the disclosure after an unarmed save, without asking Android yet", async () => {
    await saveZone();
    expect(
      await screen.findByRole("dialog", {
        name: "Run workflows when you arrive or leave",
      }),
    ).toBeTruthy();
    expect(screen.getByText("Second paragraph.")).toBeTruthy();
    expect(calls.request).toBe(0);
  });

  it("asks Android only once the disclosure is accepted, then arms the zone", async () => {
    await saveZone();
    fireEvent.click(
      await screen.findByRole("button", { name: "Choose location access" }),
    );
    await screen.findByText(/Zone saved\. The next reading/);
    expect(calls.request).toBe(1);
    // Once for the save, once to re-arm after the grant.
    expect(calls.setGeofence).toBe(2);
    expect(screen.queryByRole("dialog")).toBeNull();
  });

  it("'Not now' closes the disclosure without asking, and the button reopens it", async () => {
    await saveZone();
    fireEvent.click(await screen.findByRole("button", { name: "Not now" }));
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(calls.request).toBe(0);

    fireEvent.click(
      await screen.findByRole("button", { name: "Turn on location access" }),
    );
    expect(await screen.findByRole("dialog")).toBeTruthy();
    expect(calls.request).toBe(0);
  });
});
