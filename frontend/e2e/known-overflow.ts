// Routes that overflow horizontally today, per mobile project, with the reason.
// Measured on master at 40b6062.
//
// A test listed here is expected to fail. When a fix lands, that test starts
// passing, Playwright reports the expected failure as unexpected, and CI fails
// until the entry is deleted, so this list can only shrink.
const CANVAS_HEADER =
  "The workflow header (name, viewing-only pill, credits, Run) keeps its desktop layout and runs to 714px.";
const CONSOLE_TOP_BAR = "The top bar's Menu button sits at 336-380px.";
const ACCOUNT_MENU = "The top bar's account menu ends at 324px.";

export const KNOWN_MOBILE_OVERFLOW: Record<string, Record<string, string>> = {
  "android-360": {
    "/workflows/smoke": CANVAS_HEADER,
    "/bazaar/tendril": CONSOLE_TOP_BAR,
    "/bazaar/prism": CONSOLE_TOP_BAR,
    "/bazaar/helixbox": CONSOLE_TOP_BAR,
  },
  "android-320": {
    "/": "The landing header's Menu button sits at 304-348px.",
    "/workflows": ACCOUNT_MENU,
    "/workflows/smoke": CANVAS_HEADER,
    "/bazaar":
      "The top bar's account menu, the bottom navigation and a row's Add button end at 324-340px, so the page scrolls sideways by 4px.",
    "/bazaar/tendril": `${ACCOUNT_MENU} ${CONSOLE_TOP_BAR}`,
    "/bazaar/prism": `${ACCOUNT_MENU} ${CONSOLE_TOP_BAR}`,
    "/bazaar/helixbox": `${ACCOUNT_MENU} ${CONSOLE_TOP_BAR}`,
    "/usage": ACCOUNT_MENU,
    "/billing": ACCOUNT_MENU,
  },
};
