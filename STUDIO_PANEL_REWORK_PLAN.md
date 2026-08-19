# Studio panel rework — console, rail, topbar, hints

## Context

Four UI defects/annoyances in the workflow studio (`/workflows/[id]`), all reported from
screenshots on `master`:

1. **The bottom dock says "console" twice.** It is not a duplicate tab — it is a static
   panel title `<span>console</span>` sitting immediately beside a status `<Pill>` whose
   idle text is the literal string `"console"`. Because the pill is rounded and sits
   right before the real `LOGS` / `INSPECTOR` tab pills, it reads as a third tab. The
   dock should be a log viewer and nothing else.
2. **The right rail is chat-only.** Node config (Inspector) lives in the bottom dock, so
   clicking a node hijacks the dock, forces it open, and pushes the logs away. The rail
   should carry Chat _and_ Inspector (and Terminal, which follows Inspector out of the
   dock) behind a tab toggle in one column.
3. **The topbar `draft` pill is noise.** Deploy state is already carried by the
   `Deploy` / `Re-deploy` button label and the Run button's disabled state.
4. **The delete-node key hint renders as tofu.** The hints array hardcodes `⌫`
   (U+232B), which the mono font stack does not cover. The hints row also sits
   bottom-left, where an open console covers it.

Intended outcome: a dock that is unambiguously the log viewer, one right-hand column
that toggles between conversation and node config, a cleaner topbar, and a hints row
that is legible and out of the way at the top-left of the canvas.

**Decisions already made by the user** (do not re-litigate):

- Dock = logs only, _no exceptions_ — Terminal moves into the rail too.
- Topbar pill removed **entirely**, including the `deployed · <network>` state.
- Clicking a node does **not** switch the rail — it badges the INSPECT pill with an
  accent dot. The current `setDockTab("inspector"); setLogOpen(true)` side effect goes.
- Delete-node key becomes an **inline SVG backspace glyph**, not text.

---

## Ground rules for this codebase

- Styling is **inline `style={{}}` + CSS custom-property tokens** from
  `frontend/src/app/globals.css` (`--bg`, `--bg-elev-1..3`, `--accent`, `--fg`,
  `--fg-muted`, `--fg-dim`, `--border`, `--border-strong`, `--r-1/--r-2`,
  `--font-mono`, `--font-sans`, `--ease`). No Tailwind, no CSS modules.
- **No store for studio state.** Everything is `useState` in `CanvasPage`, passed down
  as props. `ChatConsoleHost` is a render-prop wrapper around a single
  `useChatConsole()` call so the rail and the dock share one SSE subscription.
- Icons live in `frontend/src/components/ui/index.tsx` as
  `export const IconX = ({ size = N }) => (<svg …/>)` — see `IconClose`,
  `IconSearch`, `IconGrid`. Convention: `viewBox="0 0 16 16"`,
  `fill="none"`, `stroke="currentColor"`, `strokeWidth 1.3–1.5`, `strokeLinecap="round"`.
- Per global prefs: **atomic commits**, no AI attribution trailer, run
  `npx prettier@3 --write` over touched files before committing, never force-push.

---

## Branch & PR

Cut from the freshly synced `master` (currently `2c5891d`):

```bash
git checkout master && git checkout -b feature/studio-panel-rework
```

Push to `origin` (`BonchitoSky` fork) and open the PR against `agen-mesh/master`.
Note: local `master` reports "diverged" from `origin/master` — that is the stale fork
remote, expected, ignore it.

---

## Change 1 — Right rail becomes CHAT / INSPECT / TERM

**File: `frontend/src/components/canvas/chat/ChatRail.tsx`**

Add an exported tab union next to the component (mirrors the `ConsoleTab` precedent
that is being deleted):

```ts
export type RailTab = "chat" | "inspector" | "terminal";
```

New props on `ChatRailProps` (keep all existing ones):

```ts
/** Rendered under the INSPECT tab. Always mounted (hidden via CSS) so
 *  in-progress edits survive a tab switch. */
inspectorNode: React.ReactNode;
/** Drives the accent-dot badge on the INSPECT pill — a node is selected
 *  but the user is looking at another tab. */
hasSelection: boolean;
/** Non-null only while the run holds a Tendril lease; gates the TERM pill. */
leaseId: string | null;
```

`inspectorNode` is a `ReactNode` slot rather than threading
`selected/workflowId/onUpdate/onDelete/onClose` through — it moves the _existing_
`inspectorNode` prop from `ConsolePanel` to `ChatRail` instead of inventing a second
convention, and keeps `chat/` from importing `Inspector`. `leaseId` stays a scalar
(not a prebuilt `terminalNode`) because ChatRail owns the `onClose` that flips the tab
back to Chat.

**Tab state stays local to `ChatRail`.** Nothing in `CanvasPage` drives it: the one
programmatic switch (`selectNode` → `setDockTab("inspector")`) is being deleted by
decision, and `onShowLogs` targets the dock. `ChatRail`'s position in the tree is stable
(unkeyed sibling inside the `ChatConsoleHost` fragment), so local state survives every
re-render including run start/stop. Lift it only if a second call site ever appears.

Handle the disappearing TERM pill by **deriving at render**, copying
`ConsolePanel`'s `effectiveTab` pattern — not a sync effect:

```ts
const [tab, setTab] = useState<RailTab>("chat");
// A lease that ends mid-run must not leave the rail stuck on a dead terminal.
// Derived at render rather than synced through an effect: an effect fires a
// second render pass and can lose a race with a pill click in the same tick.
const effectiveTab: RailTab = tab === "terminal" && !leaseId ? "chat" : tab;
```

Everything downstream (pill styling, pane `display`, terminal mount) reads
`effectiveTab`; only pill `onClick` and `TerminalTab.onClose` write `tab`.
The `"inspector"` arm needs **no** fallback — `inspectorNode` is unconditional and
renders its own empty state, so that tab is always valid.

Known inherited wart, worth a line in the PR body: because `tab` retains `"terminal"`,
a _new_ lease later snaps the rail back to the terminal unprompted. `ConsolePanel` has
this today. Accept it; do not add an effect.

**Header** — delete the static `chat` caption; the CHAT pill replaces it.
Layout: tab pills on the left, existing Build/Run pill on the right.

The tab-pill button style is duplicated verbatim in `ChatRail.tsx` and
`ConsolePanel.tsx`. Extract a local
`TabPill({ label, active, badge, onClick })` inside `ChatRail.tsx` — the ConsolePanel
copy dies with the dock's pill row, so a shared module is not warranted.

**Width risk — the main thing to eyeball.** At `INSPECTOR.min = 260` with header padding
`0 16px`, three pills labelled `CHAT`/`INSPECTOR`/`TERMINAL` plus the Build pill
overflow badly. Apply in this order:

1. Short labels: `CHAT` / `INSPECT` / `TERM` (~62 + 72 + 56 px + gaps).
2. Header padding `14px 16px` → `12px 12px`.
3. `flexWrap: "wrap"`, `rowGap: 6`, `columnGap: 4` so the Build pill wraps to a second
   row at the narrowest widths instead of clipping.

**Selection badge** — inside the INSPECT button, shown only when
`hasSelection && effectiveTab !== "inspector"` (suppressing it while the tab is active
gives it a natural "read" state and stops it competing with the pill's own accent
border):

```jsx
<span
  style={{
    width: 5,
    height: 5,
    borderRadius: 999,
    background: "var(--accent)",
    flexShrink: 0,
  }}
/>
```

**Body** — three panes:

```jsx
<div
  style={{ flex: 1, minHeight: 0, display: "flex", flexDirection: "column" }}
>
  {/* Chat — always mounted; ChatPane owns `draft` in local state, so
      unmounting would eat a half-typed message. */}
  <div
    style={{
      display: effectiveTab === "chat" ? "flex" : "none",
      flex: 1,
      minHeight: 0,
    }}
  >
    <ChatPane
      session={session}
      onSend={onSend}
      busy={busy}
      onShowLogs={onShowLogs}
    />
  </div>

  {/* Inspector — always mounted, same reason as the dock's version:
      in-progress field edits survive a tab switch. */}
  <div
    style={{
      display: effectiveTab === "inspector" ? "flex" : "none",
      flex: 1,
      minHeight: 0,
      flexDirection: "column",
    }}
  >
    {inspectorNode}
  </div>

  {/* Terminal — mounted only while selected: it owns a live WebSocket + SSH
      session, unlike the two above which are cheap to keep alive. */}
  {effectiveTab === "terminal" && leaseId && (
    <div style={{ flex: 1, minHeight: 0 }}>
      <TerminalTab leaseId={leaseId} onClose={() => setTab("chat")} />
    </div>
  )}
</div>
```

Rewrite the stale file-header comment — it currently claims node config lives in
the bottom dock, which this change makes false.

Pre-existing behavior to note (not a regression): a `display:none` Inspector still runs
its subtree effects and connector fetches (`toolsApi`, `oauth2`, `tendrilApi`). That is
exactly what the dock does today.

**File: `frontend/src/components/canvas/chat/ChatPane.tsx`** — delete
`borderRight: "1px solid var(--border)"`. Inside the rail it draws a line down the
window's right edge under Chat only, so the rail's right edge would flicker between tabs.

---

## Change 2 — Mount Inspector in the rail without doubled chrome

**File: `frontend/src/components/canvas/Inspector.tsx`**

Three collisions when nesting it inside `ChatRail`:

- Root hardcodes `borderLeft: "1px solid var(--border)"` and `EmptyInspector`
  repeats it → a second 1px line against `ChatRail`'s own `borderLeft`.
- `EmptyInspector` renders its own uppercase `inspector` caption →
  duplicates the INSPECT tab pill.
- `EmptyInspector`'s root has no `height`; its inner empty state relies on `flex: 1` +
  `justifyContent: center`, so with no definite height it collapses to content height
  and the "select a node" state jams to the top of a tall rail.

Add one optional prop:

```ts
/** Rendered inside a host that already draws the rail's left border and its
 *  own "INSPECT" caption (the right rail's tab pane). Drops this component's
 *  own edge chrome + caption so they aren't doubled. */
embedded?: boolean;
```

- `Inspector` root: `borderLeft: embedded ? undefined : "1px solid var(--border)"`.
  Leave `background: var(--bg-elev-1)` (matches the rail exactly), `overflow: auto`,
  `height: "100%"`, `flexShrink: 0` — they behave correctly inside a
  `flex:1; minHeight:0` pane.
- `EmptyInspector({ width, embedded })`: same `borderLeft` conditional; skip the
  caption block when `embedded`; add `height: "100%", flex: 1, minHeight: 0`
  so the centred ◇ state actually centres.

Rejected: a `marginLeft: -1` wrapper (hides the problem, breaks if border width changes);
hoisting all chrome out of `Inspector` into callers (correct long-term, far larger
refactor, out of scope).

Behavior note: the Inspector's own X (`onClose` → `setSelectedId(null)`) now clears the
selection and drops the rail into the empty state while staying on INSPECT. That reads
correctly. `EmptyInspector` still ignores `onClose`, which is fine — nothing to close.

---

## Change 3 — Bottom dock becomes logs-only

**File: `frontend/src/components/canvas/ConsolePanel.tsx`**

New prop surface:

```ts
interface ConsolePanelProps {
  open: boolean;
  onToggle: () => void;
  runId: string | null;
  running: boolean;
  logs: LogEvent[];
  elapsed: number | null;
  done: boolean;
}
```

Removed: `leaseId`, `tab`, `onTabChange`, `inspectorNode`.

Delete:

- `export type ConsoleTab` — sole importer is `CanvasPage`, which also loses it.
- `import { TerminalTab }`.
- the `effectiveTab` block and its comment.
- the static `<span>console</span>` → replace with the same span rendering
  `logs` (the existing `textTransform: "uppercase"` handles casing; source stays
  lowercase, matching how `console` was written).
- the `: "console"` fallback → `headerPill` becomes `string | null`, and the pill
  renders as `{headerPill && <Pill mono tone={pillTone} dot={running}>…}`.
  `pillTone` is unchanged.
- the whole tab-pill row, including the `onClick={(e) => e.stopPropagation()}`
  wrapper that existed only to stop pill clicks toggling the drawer.
- the inspector pane wrapper.
- the terminal block and its `onTabChange("logs")` close callback.
- `display: effectiveTab === "logs" ? "block" : "none"` → the log list is the only
  body now; collapse the flex wrapper and render the log `<div>` directly under
  `{open && …}` with `flex: 1`.

Keep untouched: the resize grip, `agentmesh_console_height` persistence,
the step count, the collapse toggle, and the placeholder
"run a workflow to see execution logs here."

Rewrite (do not just orphan) the stale comments: the `inspectorNode` doc,
the three-tab body comment, and "stays mounted when another tab is active".

**File: `frontend/src/components/canvas/CanvasPage.tsx`**

- `import { ConsolePanel } from "./ConsolePanel";` (drop the `ConsoleTab` type).
- delete `const [dockTab, setDockTab] = useState<ConsoleTab>("logs");`.
- delete `selectNode` and its 5-line comment; pass
  `setSelectedId={setSelectedId}` to `CanvasGraph`. `setSelectedId` is a stable
  setter — no `useCallback` needed.
- shrink the `ConsolePanel` call to the seven remaining props.
- the `ChatRail` call gains `inspectorNode={…}` (the JSX moved out of
  `ConsolePanel`, now with `embedded`), `hasSelection={selected !== null}`,
  and `leaseId={chat.leaseId}`.
- `onShowLogs` → `() => setLogOpen(true)`.

```jsx
inspectorNode={
  <Inspector
    selected={selected}
    workflowId={workflow.id}
    onUpdate={onUpdate}
    onDelete={onDelete}
    onClose={() => setSelectedId(null)}
    width="100%"
    embedded
  />
}
```

---

## Change 4 — Remove the topbar deploy-status pill

**File: `frontend/src/components/canvas/CanvasPage.tsx`**

- Delete the `<Pill mono dot tone={…}>{deployed ? … : "draft"}</Pill>`.
- `ALGORAND_NETWORK` and its comment become unused — grep confirms the only
  other definition is a separate one in `components/Topbar.tsx` (leave that alone).
  Delete both here, or `npm run lint` flags it under `no-unused-vars`.
- **`deployed` must stay** — still used when set from `wf.status`, as the topbar prop,
  by `CanvasGraph`, for the `Deploy`/`Re-deploy` label, and for the Run button's
  disabled + title + opacity. Keep it as a `CanvasTopbar` prop.
- The `Pill` import stays used by `{saveLabel && <Pill mono>{saveLabel}</Pill>}`.

---

## Change 5 — Hints bar to top-left + backspace icon

**File: `frontend/src/components/canvas/CanvasGraph.tsx`** (hints block)

Geometry: the graph wrapper is `position: relative; flex: 1` and sits _below_
the 52px topbar, which is outside it — so the wrapper's top edge is clean. Change
`bottom: 44` → `top: 14`, keep `left: 16`.

**Add `pointerEvents: "none"` — now load-bearing, not cosmetic.** At bottom-left it only
swallowed drag-to-pan in a dead corner. At top-left it lands on the default view origin
(`view = { x: 40, y: 40, k: 0.95 }`, so world (0,0) paints at screen (40,40)) and would
eat the first node's mousedown and click on nearly every workflow. The keycaps have no
interactive behavior, so nothing is lost.

`maxWidth: "calc(100% - 96px)"` existed to wrap away from the bottom-right zoom controls
(still at `bottom: 44; right: 16`). Nothing sits top-right; retune to
`calc(100% - 32px)`.

Widen the tuple array to `ReactNode` and move the React `key` off the icon slot:

```jsx
{([
  ["drag bg", "pan"],
  ["scroll", "zoom"],
  ["drag port", "connect"],
  [<IconBackspace key="bksp" size={12} />, "delete node"],
] as [React.ReactNode, string][]).map(([k, v]) => (
  <span key={v} title={v === "delete node" ? "Backspace" : undefined} …>
```

(`key` was `k`; `v` is unique and stable. The `title` restores the key's identity for
screen readers and anyone who doesn't recognise the glyph, now that the visible text no
longer spells `⌫`.)

**File: `frontend/src/components/ui/index.tsx`** — add `IconBackspace` beside
`IconClose`/`IconSearch`/`IconGrid`:

```jsx
export const IconBackspace = ({ size = 12 }: { size?: number }) => (
  <svg width={size} height={size} viewBox="0 0 16 16" fill="none"
       aria-hidden="true" style={{ display: "block" }}>
    <path d="M6 3.5h6.5a1.5 1.5 0 0 1 1.5 1.5v6a1.5 1.5 0 0 1-1.5 1.5H6L1.5 8z"
          stroke="currentColor" strokeWidth="1.4"
          strokeLinecap="round" strokeLinejoin="round" />
    <path d="M7.5 6.5l3 3M10.5 6.5l-3 3"
          stroke="currentColor" strokeWidth="1.4" strokeLinecap="round" />
  </svg>
);
```

12×12 inside the 18px keycap leaves ~3px optical padding, matching the ~10px cap-height
of the neighbouring 10px mono text. `stroke="currentColor"` inherits the keycap's
`--fg-muted`. `display: block` kills the inline-baseline descender gap; the keycap is
already `inline-flex` + centred on both axes, so no manual nudge.

---

## Change 6 (optional) — make the rail terminal usable

Only worth doing if a Tendril workflow is actually in play.

**Column count** at `fontSize: 12` (≈7.2px/cell, minus ~14px scrollbar):
260px → ~34 cols · 320px → ~42 · 560px → ~75 · 640px → ~87.

- `frontend/src/components/canvas/panelSizing.ts`: raise `INSPECTOR.max` 560 → **640** —
  the first round value clearing the de-facto 80-column floor. Guard check:
  `PALETTE.min (200) + 640 + MIN_CANVAS (320) = 1160`, so a ≥1160px window honours it and
  narrower windows are already clamped by `clampWidth`'s
  `containerWidth - otherPanelWidth - MIN_CANVAS` rule. Do **not** shrink the
  terminal font to buy columns.
- `frontend/src/components/canvas/TerminalTab.tsx`: the close button is a raw
  unstyled `<button style={{ fontSize: 11 }}>` — browser-default chrome, glaring at the
  top of a themed rail. Restyle to the ghost/tab-pill pattern and keep it (it does
  something the pills don't: returns to Chat _and_ reads as "done with this machine").
- Same file: `sendResize` fires on every `ResizeObserver` delivery, so a
  one-second rail drag costs ~60 `fit.fit()` reflows and ~60 PTY resize frames. Coalesce
  with a trailing `requestAnimationFrame`/`setTimeout(…, 80)` guard cleared in the effect
  teardown. Pre-existing, low severity — but the rail drag makes it a routine gesture.
- Minor: the terminal hardcodes `theme: { background: "#0b0b0d" }` against a
  `var(--bg-elev-1)` rail — a visible seam. Optional.

**No new resize trigger is needed** — the `ResizeObserver` observes the host
element (not the window), and the host is `flex: 1` inside the width-driven rail, so
dragging the rail's `ResizeHandle` already refits and notifies the PTY.

---

## Commit breakdown

Order matters: tearing down the dock first would leave one commit where Inspector and
Terminal are unreachable. Build the new home first.

1. `feat(canvas): tabbed chat/inspector/terminal right rail`
   — `ChatRail.tsx`, `ChatPane.tsx` (borderRight), `Inspector.tsx` (`embedded`),
   `CanvasPage.tsx` (wire the three new props). The dock still has its own
   inspector/terminal here — momentary duplication, but the app works at every commit.
2. `refactor(canvas): bottom dock is logs only`
   — `ConsolePanel.tsx` teardown + `CanvasPage.tsx` (`dockTab`, `selectNode`,
   `onShowLogs`, shrunk call). Removes the duplication from (1) and lands the
   "clicking a node no longer hijacks a panel" behavior change — put that in the body.
3. `refactor(canvas): drop topbar deploy-status pill`
   — `CanvasPage.tsx` only (pill + `ALGORAND_NETWORK` + comment).
4. `feat(canvas): move graph hints to top-left, add backspace icon`
   — `CanvasGraph.tsx` + `ui/index.tsx`.
5. `fix(canvas): make the rail terminal usable` _(optional)_
   — `panelSizing.ts`, `TerminalTab.tsx`.

Run `npx prettier@3 --write` over the touched files before each commit (project has no
Prettier config of its own — defaults apply).

---

## Verification

**No component/DOM tests exist in this repo** — all 11 test files are pure-logic `.ts`
under `vitest`; nothing renders `ConsolePanel`, `ChatRail`, `Inspector`, or `CanvasPage`.
Of the touched files only `panelSizing.ts` has a test
(`frontend/src/components/canvas/panelSizing.test.ts`), and its seven `clampWidth` cases
reference `PALETTE.min/max`, `INSPECTOR.default`, `MIN_CANVAS` — **none assert
`INSPECTOR.max`**, so the 560 → 640 bump needs no test edits. Optionally add
`expect(clampWidth(9999, INSPECTOR, WIDE, 280)).toBe(INSPECTOR.max)` to pin the new bound.

From `frontend/`:

```bash
npm run typecheck && npm run lint && npm test
```

`typecheck` surfaces every dropped prop and the `ConsoleTab` deletion; `lint` catches the
unused `ALGORAND_NETWORK`.

Then run the dev server (`.claude/launch.json` → `frontend-dev`; the `dev` script pins
`-p 3100` even though launch.json declares 3000) and open a workflow. Manual checks:

- Dock header reads `LOGS` once, with no idle pill and no tab pills; the run pill appears
  only after a run starts; collapse/expand and drag-resize still work.
- Rail header at exactly 260px — pills must not clip or overlap the Build/Run pill.
- Click a node: rail **stays on Chat**, INSPECT pill grows an accent dot, dock does not
  pop open. Switch to INSPECT and confirm the config renders with a single left border
  and no duplicate "inspector" caption.
- Half-typed chat text survives a Chat → Inspect → Chat round trip; a half-edited
  Inspector field survives Inspect → Chat → Inspect.
- With no node selected, INSPECT shows the empty state **vertically centred**.
- Topbar has no `draft`/`deployed` pill; `Deploy`/`Re-deploy` label and the Run button's
  disabled state still track deploy status.
- Hints row sits top-left, the backspace glyph renders as an icon (not tofu), and a node
  dragged underneath the row still receives mousedown and selects.
- Tendril workflow only: TERM pill appears when a lease exists, the terminal mounts on
  select and unmounts on switch away, and an expiring lease drops the rail back to Chat
  without a stuck dead tab.

---

## Follow-ups (not in this PR)

- Rename `chat/ChatRail.tsx` → `canvas/RightRail.tsx`. It is no longer "the chat rail"
  and it now imports `../TerminalTab`, inverting the folder's containment. Skipped here
  only because the import churn would bury the four real changes in the diff.
- Hoisting all edge chrome out of `Inspector` into its callers, so `embedded` isn't
  needed at all.
