import { afterEach, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";

const push = vi.hoisted(() => vi.fn());
vi.mock("next/navigation", () => ({
  useRouter: () => ({ push }),
  usePathname: () => "/workflows",
}));
vi.mock("@/hooks/useAuth", () => ({
  useAuth: () => ({ user: { name: "Morgan", email: "morgan@example.test" } }),
}));
vi.mock("@/hooks/useIsHandheld", () => ({ useIsHandheld: () => false }));
vi.mock("@/lib/nativeAuth", () => ({ IS_NATIVE: false }));
import { Topbar } from "./Topbar";

afterEach(() => { cleanup(); vi.clearAllMocks(); vi.unstubAllGlobals(); });

it("opens account deletion directly from the signed-in profile icon", () => {
  vi.stubGlobal("matchMedia", () => ({ matches: false, addEventListener: vi.fn(), removeEventListener: vi.fn() }));
  render(<Topbar />);
  expect(screen.queryByRole("button", { name: "Delete account" })).toBeNull();
  fireEvent.click(screen.getByRole("button", { name: "Account menu" }));
  fireEvent.click(screen.getByRole("button", { name: "Delete account" }));
  expect(push).toHaveBeenCalledWith("/settings");
  expect(screen.queryByRole("button", { name: "Delete account" })).toBeNull();
});
