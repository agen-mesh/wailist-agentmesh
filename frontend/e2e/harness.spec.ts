import { readdirSync } from "node:fs";
import { join, relative, sep } from "node:path";
import { expect, test } from "@playwright/test";
import {
  collectPageErrors,
  horizontalOffenders,
  openRoute,
  settle,
} from "./helpers/page";
import { ALL_ROUTES, pageRoute, SHARE_TOKEN, WORKFLOW_ID } from "./routes";

test("the route inventory covers current page modules", () => {
  const app = join(__dirname, "../src/app");
  const routes: string[] = [];
  const visit = (directory: string) => {
    for (const entry of readdirSync(directory, { withFileTypes: true })) {
      const path = join(directory, entry.name);
      if (entry.isDirectory()) visit(path);
      else if (entry.name === "page.tsx") {
        routes.push(pageRoute(relative(app, directory).split(sep)));
      }
    }
  };
  visit(app);

  expect(new Set(ALL_ROUTES).size).toBe(ALL_ROUTES.length);
  expect([...ALL_ROUTES].sort()).toEqual(
    routes.filter((route) => route !== "/auth/callback").sort(),
  );
});

test("dynamic route samples are explicit and unknown parameters fail", () => {
  expect(pageRoute(["(workspace)", "workflows", "[id]"])).toBe(`/workflows/${WORKFLOW_ID}`);
  expect(pageRoute(["s", "[token]"])).toBe(`/s/${SHARE_TOKEN}`);
  expect(() => pageRoute(["new", "[unknown]"])).toThrow("No smoke sample");
  expect(() => pageRoute(["new", "[...slug]"])).toThrow("No smoke sample");
});

test("page-error collection detects an uncaught error after a clean load", async ({
  page,
}) => {
  const errors = collectPageErrors(page);
  await openRoute(page, "/signin");
  await settle(page);
  expect(errors).toEqual([]);

  const message = "smoke page-error control";
  const nextError = page.waitForEvent("pageerror");
  await page.evaluate((text) => {
    setTimeout(() => {
      throw new Error(text);
    }, 0);
  }, message);

  expect((await nextError).message).toBe(message);
  expect(errors).toEqual([message]);
});

test("overflow collection detects an added element and clears after removal", async ({
  page,
}) => {
  await openRoute(page, "/signin");
  await settle(page);
  expect(await horizontalOffenders(page)).toEqual([]);

  await page.evaluate(() => {
    const element = document.createElement("div");
    element.id = "smoke-overflow-control";
    element.textContent = "smoke overflow control";
    Object.assign(element.style, {
      position: "absolute",
      left: "0",
      top: "0",
      width: `${window.innerWidth + 80}px`,
      height: "20px",
    });
    document.body.append(element);
  });
  expect(await horizontalOffenders(page)).toEqual(
    expect.arrayContaining([
      expect.objectContaining({
        element: expect.stringContaining("smoke overflow control"),
      }),
    ]),
  );

  await page.evaluate(() => {
    document.getElementById("smoke-overflow-control")?.remove();
  });
  expect(await horizontalOffenders(page)).toEqual([]);
});

for (const region of [
  { name: "ordinary vertical scroller", className: "", shouldOverflow: true },
  { name: "chat table", className: "chat-table-wrap", shouldOverflow: false },
  { name: "display math", className: "katex-display", shouldOverflow: false },
  { name: "code block", className: "chat-code-block__body", shouldOverflow: false },
]) {
  test(`overflow collection distinguishes ${region.name}`, async ({ page }) => {
    await openRoute(page, "/signin");
    await settle(page);
    expect(await horizontalOffenders(page)).toEqual([]);

    const geometry = await page.evaluate(({ className }) => {
      const host = document.createElement("div");
      host.id = "smoke-scroll-control";
      host.className = "chat-markdown";
      Object.assign(host.style, {
        position: "fixed",
        left: "0",
        top: "0",
        width: `${window.innerWidth}px`,
      });
      const scroller = document.createElement("div");
      scroller.className = className;
      scroller.style.height = "40px";
      if (!className) scroller.style.overflow = "auto";
      const child = document.createElement("div");
      child.textContent = "oversized scrolling child";
      child.style.width = `${window.innerWidth + 80}px`;
      child.style.height = "80px";
      scroller.append(child);
      host.append(scroller);
      document.body.append(host);
      return {
        viewport: document.documentElement.clientWidth,
        left: scroller.getBoundingClientRect().left,
        right: scroller.getBoundingClientRect().right,
        childRight: child.getBoundingClientRect().right,
        overflowX: getComputedStyle(scroller).overflowX,
        clientHeight: scroller.clientHeight,
        scrollHeight: scroller.scrollHeight,
      };
    }, region);

    try {
      expect(geometry.left).toBe(0);
      expect(geometry.right).toBeLessThanOrEqual(geometry.viewport);
      expect(geometry.childRight).toBeGreaterThan(geometry.viewport + 1);
      expect(geometry.overflowX).toBe("auto");
      expect(geometry.scrollHeight).toBeGreaterThan(geometry.clientHeight);
      const offenders = await horizontalOffenders(page);
      if (region.shouldOverflow) {
        expect(offenders).toEqual(
          expect.arrayContaining([
            expect.objectContaining({
              element: expect.stringContaining("oversized scrolling child"),
            }),
          ]),
        );
      } else {
        expect(offenders).toEqual([]);
      }
    } finally {
      await page.evaluate(() => {
        document.getElementById("smoke-scroll-control")?.remove();
      });
      expect(await horizontalOffenders(page)).toEqual([]);
    }
  });
}

test("an intentional horizontal scroller must itself fit the viewport", async ({
  page,
}) => {
  await openRoute(page, "/signin");
  await settle(page);
  expect(await horizontalOffenders(page)).toEqual([]);
  await page.evaluate(() => {
    const scroller = document.createElement("div");
    scroller.id = "smoke-wide-scroller";
    scroller.className = "chat-code-block__body";
    scroller.textContent = "wide horizontal scroller";
    Object.assign(scroller.style, {
      position: "fixed",
      left: "0",
      top: "0",
      width: `${window.innerWidth + 80}px`,
      height: "20px",
    });
    document.body.append(scroller);
  });
  try {
    expect(await horizontalOffenders(page)).toEqual(
      expect.arrayContaining([
        expect.objectContaining({
          element: expect.stringContaining("wide horizontal scroller"),
        }),
      ]),
    );
  } finally {
    await page.evaluate(() => {
      document.getElementById("smoke-wide-scroller")?.remove();
    });
    expect(await horizontalOffenders(page)).toEqual([]);
  }
});

for (const control of [
  { name: "visible nowrap text", mode: "visible", className: "", spills: true },
  { name: "left-indented text", mode: "left", className: "", spills: true },
  { name: "ordinary vertical scroller", mode: "vertical", className: "", spills: true },
  { name: "wrapped text", mode: "wrapped", className: "", spills: false },
  { name: "hidden text", mode: "hidden", className: "", spills: false },
  { name: "transparent ancestor", mode: "transparent", className: "", spills: false },
  { name: "chat table text", mode: "horizontal", className: "chat-table-wrap", spills: false },
  { name: "display math text", mode: "horizontal", className: "katex-display", spills: false },
  { name: "code block text", mode: "horizontal", className: "chat-code-block__body", spills: false },
]) {
  test(`text overflow collection distinguishes ${control.name}`, async ({ page }) => {
    await openRoute(page, "/signin");
    await settle(page);
    expect(await horizontalOffenders(page)).toEqual([]);

    const geometry = await page.evaluate(({ mode, className }) => {
      const host = document.createElement("div");
      host.id = "smoke-text-control";
      host.className = "chat-markdown";
      Object.assign(host.style, {
        position: "fixed",
        left: "0",
        top: "0",
        width: `${document.documentElement.clientWidth}px`,
      });
      if (mode === "vertical") {
        host.style.overflow = "auto";
        host.style.height = "40px";
      }
      if (mode === "transparent") host.style.opacity = "0";

      const element = document.createElement("div");
      element.className = className;
      element.setAttribute("aria-label", "text overflow control");
      element.textContent = mode === "left"
        ? "text overflow control"
        : "text overflow control ".repeat(24);
      Object.assign(element.style, {
        boxSizing: "border-box",
        width: "100%",
        margin: "0",
        padding: "0",
        font: "16px monospace",
        lineHeight: "20px",
        whiteSpace: mode === "wrapped" ? "normal" : "nowrap",
        textAlign: "left",
        textIndent: mode === "left" ? "-80px" : "0",
      });
      if (mode !== "horizontal") element.style.overflow = "visible";
      if (mode === "vertical") element.style.height = "80px";
      if (mode === "hidden") element.style.visibility = "hidden";
      host.append(element);
      document.body.append(host);

      const border = element.getBoundingClientRect();
      const range = document.createRange();
      range.selectNodeContents(element);
      const textRects = Array.from(range.getClientRects());
      return {
        viewport: document.documentElement.clientWidth,
        borderLeft: border.left,
        borderRight: border.right,
        textLeft: Math.min(...textRects.map((rect) => rect.left)),
        textRight: Math.max(...textRects.map((rect) => rect.right)),
        textRectCount: textRects.length,
        overflowX: getComputedStyle(element).overflowX,
        visibility: getComputedStyle(element).visibility,
        ancestorOpacity: getComputedStyle(host).opacity,
      };
    }, control);

    try {
      expect(geometry.borderLeft).toBe(0);
      expect(geometry.borderRight).toBeLessThanOrEqual(geometry.viewport);
      expect(geometry.textRectCount).toBeGreaterThan(0);
      if (control.mode === "wrapped") {
        expect(geometry.textRectCount).toBeGreaterThan(1);
        expect(geometry.textRight).toBeLessThanOrEqual(geometry.viewport + 1);
      } else if (control.mode === "left") {
        expect(geometry.textLeft).toBeLessThan(-1);
        expect(geometry.textRight).toBeLessThanOrEqual(geometry.viewport);
      } else {
        expect(geometry.textRight).toBeGreaterThan(geometry.viewport + 1);
      }
      if (control.mode === "horizontal") expect(geometry.overflowX).toBe("auto");
      if (control.mode === "hidden") expect(geometry.visibility).toBe("hidden");
      if (control.mode === "transparent") expect(geometry.ancestorOpacity).toBe("0");

      const offenders = await horizontalOffenders(page);
      if (control.spills) {
        expect(offenders).toEqual(
          expect.arrayContaining([
            expect.objectContaining({
              element: expect.stringContaining("[text overflow control]"),
            }),
          ]),
        );
        const offender = offenders.find((entry) => entry.element.includes("[text overflow control]"));
        expect(offender?.left).toBe(Math.round(Math.min(geometry.borderLeft, geometry.textLeft)));
        expect(offender?.right).toBe(Math.round(Math.max(geometry.borderRight, geometry.textRight)));
      } else {
        expect(offenders).toEqual([]);
      }
    } finally {
      await page.evaluate(() => {
        document.getElementById("smoke-text-control")?.remove();
      });
      expect(await horizontalOffenders(page)).toEqual([]);
    }
  });
}
