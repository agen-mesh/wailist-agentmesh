import { describe, it, expect, vi, beforeAll, afterAll, beforeEach, afterEach } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";

// Play's background-location policy: the prominent disclosure must be on
// screen BEFORE Android's permission dialog, and the dialog must only follow a
// tap on it. DISCLOSURE was written for that and then never rendered -- the
// "Turn on location access" button went straight to the system dialog. These
// tests drive the screen the way a reviewer's video does and pin the order.

const calls = { locate: 0, request: 0, setGeofence: 0, settings: 0 };
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
vi.mock("@/native/permissions", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/native/permissions")>()),
  requestBackgroundLocation: async () => {
    calls.request += 1;
    return "granted";
  },
  openSettings: async () => {
    calls.settings += 1;
  },
}));

let GeofenceScreen: typeof import("./GeofenceScreen").GeofenceScreen;

const dialogMethods = new Map(
  ["showModal", "close"].map((name) => [
    name,
    Object.getOwnPropertyDescriptor(HTMLDialogElement.prototype, name),
  ]),
);

beforeAll(() => {
  Object.defineProperties(HTMLDialogElement.prototype, {
    showModal: { configurable: true, value(this: HTMLDialogElement) { this.open = true; } },
    close: { configurable: true, value(this: HTMLDialogElement) { this.open = false; } },
  });
});

afterAll(() => {
  for (const [name, descriptor] of dialogMethods) {
    if (descriptor) Object.defineProperty(HTMLDialogElement.prototype, name, descriptor);
    else Reflect.deleteProperty(HTMLDialogElement.prototype, name);
  }
});

beforeEach(async () => {
  calls.locate = 0;
  calls.request = 0;
  calls.setGeofence = 0;
  calls.settings = 0;
  armedAfterGrant = true;
  Object.defineProperty(navigator, "geolocation", {
    configurable: true,
    value: {
      getCurrentPosition: (ok: PositionCallback) => {
        calls.locate += 1;
        ok({
          coords: { latitude: 22.57, longitude: 88.36, accuracy: 10 },
        } as GeolocationPosition);
      },
    },
  });
  ({ GeofenceScreen } = await import("./GeofenceScreen"));
});

afterEach(async () => {
  cleanup();
  await waitFor(() => expect(window.history.state?.__amSheet).not.toBe(true));
});

async function saveZone() {
  render(<GeofenceScreen workflowId="wf1" />);
  fireEvent.click(
    await screen.findByRole("button", { name: "Use my location" }),
  );
  fireEvent.click(await screen.findByRole("button", { name: "Choose location access" }));
  fireEvent.click(await screen.findByRole("button", { name: "Save zone" }));
}

describe("GeofenceScreen background-location disclosure", () => {
  it("shows a modal before the first position request and permits declining", async () => {
    render(<GeofenceScreen workflowId="wf1" />);
    fireEvent.click(await screen.findByRole("button", { name: "Use my location" }));
    expect(calls.locate).toBe(0);
    expect(calls.request).toBe(0);
    const dialog = await screen.findByRole("dialog");
    expect(dialog.tagName).toBe("DIALOG");
    expect(dialog.getAttribute("aria-modal")).toBe("true");
    fireEvent.click(screen.getByRole("button", { name: "Not now" }));
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(calls.locate).toBe(0);
    expect(calls.request).toBe(0);
  });

  it("requests only the current position after accepting the first disclosure", async () => {
    render(<GeofenceScreen workflowId="wf1" />);
    fireEvent.click(await screen.findByRole("button", { name: "Use my location" }));
    fireEvent.click(await screen.findByRole("button", { name: "Choose location access" }));
    await waitFor(() => expect(calls.locate).toBe(1));
    expect(calls.request).toBe(0);
    expect(calls.setGeofence).toBe(0);
    expect(screen.queryByRole("dialog")).toBeNull();
  });

  it("allows dismissing the first disclosure with Escape without requesting location", async () => {
    render(<GeofenceScreen workflowId="wf1" />);
    fireEvent.click(await screen.findByRole("button", { name: "Use my location" }));
    await screen.findByRole("dialog");
    fireEvent.keyDown(document, { key: "Escape" });
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(calls.locate).toBe(0);
    expect(calls.request).toBe(0);
  });

  it("shows the disclosure after an unarmed save, without asking Android yet", async () => {
    await saveZone();
    expect(
      await screen.findByRole("dialog", {
        name: "Run workflows when you arrive or leave",
      }),
    ).toBeTruthy();
    expect(screen.getByText(/This phone sends each crossing position/)).toBeTruthy();
    expect(screen.getByText(/24 hours old are discarded when the app next processes the queue/)).toBeTruthy();
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
