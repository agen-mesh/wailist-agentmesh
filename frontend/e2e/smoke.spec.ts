import { expect, test } from "@playwright/test";
import {
  collectPageErrors,
  horizontalOffenders,
  openRoute,
  settle,
} from "./helpers/page";
import { ALL_ROUTES, PROTECTED_ROUTES, SHARE_TOKEN, WORKFLOW_ID } from "./routes";

const isDesktop = (project: string) => project.startsWith("desktop");
const protectedRoutes: readonly string[] = PROTECTED_ROUTES;

// src/middleware.ts lets a request through to a protected route only when this
// cookie is present; useAuth sets it on sign-in. In mock mode the rest of the
// app treats the session as signed in.
test.beforeEach(async ({ context, baseURL }) => {
  await context.addCookies([
    { name: "agentmesh_ui", value: "1", url: baseURL ?? "" },
  ]);
});

test("public share preview accepts a request without a session cookie", async ({ context, page }) => {
  await context.clearCookies();
  expect(await context.cookies()).toEqual([]);
  const errors = collectPageErrors(page);
  const response = await openRoute(page, `/s/${SHARE_TOKEN}`);
  expect(response?.status()).toBe(200);
  expect(response?.request().headers()["cookie"] ?? "").not.toContain("agentmesh_ui");
  expect(new URL(page.url()).pathname).toBe(`/s/${SHARE_TOKEN}`);
  await settle(page);
  await expect(page.getByRole("heading", { level: 1 })).toBeVisible();
  expect(errors).toEqual([]);
});

test("private link management redirects an anonymous visitor", async ({ context, page }) => {
  await context.clearCookies();
  await openRoute(page, "/account/links");
  const url = new URL(page.url());
  expect(url.pathname).toBe("/signin");
  expect(url.searchParams.get("next")).toBe("/account/links");
});

for (const route of ALL_ROUTES) {
  test(`${route} renders without an uncaught error`, async ({ page }) => {
    const errors = collectPageErrors(page);
    const response = await openRoute(page, route);
    expect(response?.status(), "HTTP status").toBeLessThan(400);
    if (protectedRoutes.includes(route)) {
      expect(new URL(page.url()).pathname, "not sent to sign-in").not.toBe(
        "/signin",
      );
    }
    await settle(page);
    await expect(page.getByText("Application error")).toHaveCount(0);
    expect(errors, "uncaught page errors").toEqual([]);
  });
}

test("every palette tab renders", async ({ page }, testInfo) => {
  test.skip(
    !isDesktop(testInfo.project.name),
    "the palette belongs to the desktop editor",
  );
  const errors = collectPageErrors(page);
  await openRoute(page, `/workflows/${WORKFLOW_ID}`);

  // The tabs are sibling buttons. Start from the first and select each one in
  // turn, so a tab added or removed later is covered without editing this test.
  const first = page.getByRole("button", { name: "Triggers", exact: true });
  await expect(first).toBeVisible();
  const tabs = first.locator("xpath=..").getByRole("button");
  const count = await tabs.count();
  expect(count, "number of palette tabs").toBeGreaterThan(1);

  for (let i = 0; i < count; i++) {
    const tab = tabs.nth(i);
    const label = ((await tab.textContent()) ?? "").trim();
    await tab.click();
    await settle(page);
    await expect(
      page.getByText("Application error"),
      `after selecting "${label}"`,
    ).toHaveCount(0);
    expect(errors, `uncaught errors after selecting "${label}"`).toEqual([]);
  }
});

for (const route of ALL_ROUTES) {
  test(`${route} has no horizontal overflow on a phone`, async ({
    page,
  }, testInfo) => {
    test.skip(isDesktop(testInfo.project.name), "phone widths only");
    await openRoute(page, route);
    await settle(page);
    expect(
      await horizontalOffenders(page),
      "elements past the edge of the viewport",
    ).toEqual([]);
  });
}
