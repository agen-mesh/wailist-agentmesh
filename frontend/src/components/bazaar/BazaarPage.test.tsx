import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { BazaarPage as Page, BazaarResource } from "@/lib/bazaar";

const mocks = vi.hoisted(() => ({ list: vi.fn() }));
vi.mock("@/lib/bazaar", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/lib/bazaar")>()),
  bazaar: { list: mocks.list },
}));
vi.mock("@/components/Topbar", () => ({ Topbar: () => null }));
vi.mock("next/navigation", () => ({ useRouter: () => ({ push: vi.fn() }) }));

import { BazaarPage } from "./BazaarPage";

function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (reason: Error) => void;
  const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no; });
  return { promise, resolve, reject };
}

const endpoint: BazaarResource = {
  id: "catalogue-entry", url: "https://catalogue.example.test/search", method: "POST",
  description: "Search public records", merchantId: "merchant", network: "algorand",
  testnet: false, amountMicros: 10000, asset: "USDC", payTo: "merchant",
  params: [], settleCount: 10, host: "catalogue.example.test", supported: false,
};
const partner: BazaarResource = {
  ...endpoint, id: "partner-entry", host: "partner.example.test", supported: true,
  console: "tendril", provider: "Tendril",
};
const pageOf = (items: BazaarResource[]): Page => ({ items, total: items.length, offset: 0, limit: 10, supportedCount: 0 });

beforeEach(() => { mocks.list.mockReset(); });
afterEach(cleanup);

describe("independent Bazaar requests", () => {
  it("renders and filters the catalogue while partners are pending", async () => {
    const supported = deferred<Page>();
    mocks.list.mockImplementation(({ supported: isPartner }: { supported: boolean }) =>
      isPartner ? supported.promise : Promise.resolve(pageOf([endpoint])),
    );
    render(<BazaarPage />);
    await screen.findByText(endpoint.description);
    fireEvent.change(screen.getByRole("combobox", { name: "Sort endpoints" }), { target: { value: "name" } });
    await waitFor(() => expect(mocks.list).toHaveBeenCalledWith(expect.objectContaining({ supported: false, sort: "name" })));
    fireEvent.change(screen.getByRole("textbox", { name: "Search endpoints" }), { target: { value: "records" } });
    await waitFor(() => expect(mocks.list).toHaveBeenCalledWith(expect.objectContaining({ supported: false, q: "records" })));
    expect((screen.getByRole("combobox", { name: "Sort endpoints" }) as HTMLSelectElement).disabled).toBe(true);
    expect(mocks.list.mock.calls.filter(([options]) => options.supported)).toHaveLength(1);
    await act(async () => supported.resolve(pageOf([partner])));
    expect(screen.getByText(endpoint.description)).toBeTruthy();
    expect(screen.getByText("Tendril", { exact: true })).toBeTruthy();
  });

  // The Prev/Next toolbar was written here first; ui/Pager.tsx is it
  // extracted, so this page has to keep working through the shared component
  // rather than a second copy of it.
  it("pages the catalogue through the shared Pager", async () => {
    mocks.list.mockImplementation(
      ({ supported: isPartner }: { supported: boolean }) =>
        isPartner
          ? Promise.resolve(pageOf([]))
          : Promise.resolve({ ...pageOf([endpoint]), total: 24 }),
    );
    render(<BazaarPage />);
    await screen.findByText(endpoint.description);
    // Pager's own wording and page-size control, not the inline copy.
    expect(screen.getByText(/24 endpoints/)).toBeTruthy();
    expect(screen.getByText("Page 1 of 3")).toBeTruthy();
    expect(screen.getByLabelText("endpoints per page")).toBeTruthy();
    // Paging here is a refetch, not a slice: the request has to change.
    fireEvent.click(screen.getByRole("button", { name: /next/i }));
    await waitFor(() =>
      expect(mocks.list).toHaveBeenCalledWith(
        expect.objectContaining({ supported: false, offset: 10, limit: 10 }),
      ),
    );
    fireEvent.change(screen.getByLabelText("endpoints per page"), {
      target: { value: "5" },
    });
    await waitFor(() =>
      expect(mocks.list).toHaveBeenCalledWith(
        expect.objectContaining({ supported: false, offset: 0, limit: 5 }),
      ),
    );
  });

  it("shows a catalogue error and retries before partners settle", async () => {
    const supported = deferred<Page>();
    const catalogue = vi.fn().mockRejectedValueOnce(new Error("Catalogue unavailable")).mockResolvedValue(pageOf([endpoint]));
    mocks.list.mockImplementation(({ supported: isPartner }: { supported: boolean }) => isPartner ? supported.promise : catalogue());
    render(<BazaarPage />);
    await screen.findByText("Catalogue unavailable");
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    await screen.findByText(endpoint.description);
    expect(screen.queryByText("Catalogue unavailable")).toBeNull();
    expect(catalogue).toHaveBeenCalledTimes(2);
    expect(mocks.list.mock.calls.filter(([options]) => options.supported)).toHaveLength(1);
  });

  it("fills partners while the catalogue is still pending", async () => {
    const catalogue = deferred<Page>();
    mocks.list.mockImplementation(({ supported: isPartner }: { supported: boolean }) => isPartner ? Promise.resolve(pageOf([partner])) : catalogue.promise);
    render(<BazaarPage />);
    await screen.findByText("Tendril", { exact: true });
    expect(screen.getByText("Loading…")).toBeTruthy();
    expect(screen.getByRole("textbox", { name: "Search endpoints" })).toBeTruthy();
    await act(async () => catalogue.resolve(pageOf([endpoint])));
    expect(screen.getByText(endpoint.description)).toBeTruthy();
  });

  // The guarantee is that a late partner result never shoves the independent
  // catalogue DOWN the page. This used to be enforced by pinning the partner
  // region to height: 320 forever, which held it open even once three ~210px
  // cards had landed -- a permanent ~110px hole above "Everything else" that
  // read as a section failing to render. So the box is now reserved only while
  // the request is in flight, and the assertions below moved with it: the
  // reservation is checked WHILE PENDING, and afterwards the region is
  // required to be no taller than it was, never taller.
  it.each(["success", "empty", "error"] as const)("preserves partner space on %s", async (outcome) => {
    const supported = deferred<Page>();
    mocks.list.mockImplementation(({ supported: isPartner }: { supported: boolean }) => isPartner ? supported.promise : Promise.resolve(pageOf([endpoint])));
    const view = render(<BazaarPage />);
    await screen.findByText(endpoint.description);
    const frame = screen.getByRole("region", { name: "Partner services" });

    // Pending: the space is held, so the catalogue below has somewhere to sit.
    const reserved = Number.parseFloat(getComputedStyle(frame).height);
    expect(reserved).toBeGreaterThan(0);
    expect(getComputedStyle(frame).overflowY).toBe("auto");
    expect(frame.getAttribute("tabindex")).toBe("0");

    const row = view.container.querySelector(".bz-row");
    await act(async () => {
      if (outcome === "error") supported.reject(new Error("Partner service unavailable"));
      else supported.resolve(pageOf(outcome === "success" ? [partner] : []));
    });

    // Settled: same node, catalogue intact and never pushed further down.
    expect(screen.getByRole("region", { name: "Partner services" })).toBe(frame);
    expect(view.container.querySelector(".bz-row")).toBe(row);
    const settled = Number.parseFloat(getComputedStyle(frame).height) || 0;
    expect(settled).toBeLessThanOrEqual(reserved);

    // And the reservation is released rather than left holding an empty box.
    expect(getComputedStyle(frame).height).not.toBe(`${reserved}px`);
    expect(frame.getAttribute("tabindex")).toBeNull();

    if (outcome === "empty") expect(screen.getByText("No partners available.")).toBeTruthy();
    if (outcome === "error") expect(screen.getByText("Could not load partners.")).toBeTruthy();
    expect(screen.queryByText(endpoint.description)).toBeTruthy();
  });
});
