# Workflow chat panel — research + implementation workflow

Goal, in the devs' words: _a chat window on the workflow page like n8n's, where the
responses can be read by coders who need them but aren't important for normal people._

That sentence is a **progressive-disclosure requirement**, not a chat-widget
requirement. The chat window is the easy half. The hard half is that one panel has
to serve two audiences at once: someone who wants to read an answer, and someone
who wants to know which tool call cost $0.0043 and what JSON came back.

This document is the research (Part 1), the read of where AgentMesh stands today
(Part 2), the design that follows from both (Part 3), and the build order with
commit breakdown (Part 4).

---

## Part 1 — Research: how n8n actually does it

Sources: n8n docs (Chat Trigger, Chat node, streaming responses, agent building)
and, more usefully, the n8n editor source itself —
`packages/frontend/editor-ui/src/features/execution/logs/` on `n8n-io/n8n@master`.
The docs describe the feature; the source describes the design. Findings below are
from the source unless noted.

### 1.1 It is not a chat window. It is a three-pane console with chat as one pane.

n8n's `LogsPanel.vue` is a bottom drawer containing three horizontally-arranged,
independently resizable panes:

```
┌──────────────────────────────────────────────────────────────────────────┐
│  Chat            │  Logs overview          │  Log details                │
│  (conversation)  │  (tree of node runs)    │  (raw input / output)       │
│                  │                         │                             │
│  > summarise…    │  ▾ AI Agent   1.2s  ▸   │  INPUT   { … }              │
│  ◂ Here's the…   │    ├ OpenAI   0.9s      │  OUTPUT  { … }              │
│                  │    └ Serper   0.2s      │                             │
│  [type here… ]   │  ▸ Respond    0.0s      │                             │
└──────────────────────────────────────────────────────────────────────────┘
```

The critical structural insight: **chat and logs are siblings in one surface, not
separate features.** The chat pane is the human view of the same execution the
logs pane details. That adjacency is what lets one panel serve both audiences —
a non-technical user reads the left pane and never looks right; a developer
reads across.

### 1.2 Three panel states, not two

`logs.constants.ts`:

```ts
export const LOGS_PANEL_STATE = {
  CLOSED: "closed", // 32px header strip only
  ATTACHED: "attached", // docked bottom drawer
  FLOATING: "floating", // torn off into a separate browser window
} as const;
```

`FLOATING` (`usePopOutWindow.ts`) opens the whole panel in a real `window.open`
child document so a developer can watch logs on a second monitor while the canvas
stays full-screen. Notable but expensive; correctly a later phase.

### 1.3 Concrete layout numbers (`useLogsPanelLayout.ts`)

Worth copying rather than re-deriving — these are tuned:

| Panel               | Default         | Min   | Max                      |
| ------------------- | --------------- | ----- | ------------------------ |
| Drawer height       | 30% of viewport | 160px | 75% of viewport          |
| Collapsed header    | 32px            | —     | —                        |
| Chat pane width     | `min(800, 30%)` | 240px | 80%                      |
| Logs overview width | `min(240, 20%)` | 80px  | 500px, **or full-width** |

Two details that matter:

- Each dimension persists to its own `localStorage` key
  (`N8N_CANVAS_CHAT_HEIGHT`, `N8N_CANVAS_CHAT_WIDTH`,
  `N8N_LOGS_OVERVIEW_PANEL_WIDTH`).
- The overview pane has `allowFullSize: true`. Dragging it to full width
  **deselects the current log entry and closes the details pane**
  (`handleResizeOverviewPanelEnd`). Resizing is the deselect gesture — no
  separate close button.

### 1.4 The log tree is recursive, and that is the whole point

`logs.types.ts` models a log entry as a tree node, not a row:

```ts
type BaseLogEntry = {
  parent?: LogEntry;
  children: LogEntry[];
  runIndex: number;
  consumedTokens: LlmTokenUsageData;
  executionId: string;
  isSubExecution: boolean;
};
export type LogEntry = NodeLogEntry | GroupLogEntry;
```

An agent's LLM calls and tool calls are **children of the agent's row**, and
sub-workflow executions nest further. This is what makes a 40-event agent run
readable: collapsed, it is one line; expanded, it is the full call tree. A flat
list — which is what AgentMesh has today — cannot express "this tool call happened
_because of_ that agent step."

`LogsOverviewRow.vue` renders per row: indent connectors, node icon, name,
status + duration, started-at, **subtree-aggregated token count**, an
"open node" button, and a **run-this-node-only** (partial execution) button.
Token counts aggregate up the subtree and hide when a row is expanded (children
show their own).

### 1.5 Selection is the bridge between the two audiences

- Chat message → `displayExecution(executionId)` routes to that message's execution.
- Log row → `handleOpenNdv()` opens the full node editor, pre-seeded with the
  right input branch and run index.
- Keyboard: `j`/`k` and arrows to move, `Space` to expand, `Enter` to open the
  node, `Escape` to deselect; `i`/`o` toggle input/output in the popped-out window.
- `isLogSelectionSyncedWithCanvas` — an opt-in toggle that highlights the selected
  log row's node on the canvas.

Every artefact in the human pane is a hyperlink into the technical pane. That is
the mechanism behind "coders can read it, normal people don't have to."

### 1.6 Execution model: one message = one execution

From the Chat Trigger docs, verbatim: _"Every message to the Chat Trigger executes
your workflow"_ — 10 messages consume 10 executions. Conversation continuity is
**not** implicit; it requires opting into `Load Previous Session` **and** wiring
the Chat Trigger and the Agent to the _same memory sub-node_.

This is important for us: n8n's chat is a **sequence of independent executions
presented as a conversation**. Memory is a separate, explicit feature. We can ship
the same model honestly.

### 1.7 Response modes and streaming

Chat Trigger `responseMode`:

1. `lastNode` — the final node's output becomes the reply.
2. `responseNodes` — a Chat / Respond-to-Webhook node formats the reply.
3. `streaming` — tokens stream as generated.

Streaming has a documented sharp edge: _"Even with streaming enabled on the
trigger, you need at least one node configured to stream data. Otherwise, your
workflow will send no data."_ The trigger flag alone is not enough.

The **Chat node** (`n8n-nodes-langchain.chat`) additionally supports mid-execution
messages — `Send Message` (fire and continue) and `Send and Wait for Response`
(pause for free-text or approve/reject buttons), i.e. human-in-the-loop. It
requires `responseMode: responseNodes` and is unsupported in embedded mode.

### 1.8 Session handling

`useChatState.ts`: a session id is generated client-side, displayed truncated
(`abc12...`) in the chat header, click-to-copy, with a reset button that clears the
session id, the messages, the execution data, and any partial-execution
destination. `beforeMessageSent` re-registers the chat webhook before _every_
message — a fresh webhook with a full timeout per message rather than one
long-lived registration.

### 1.9 What we should take, and what we should not

**Take:** the three-pane structure; chat-as-a-pane; the recursive log tree; the
selection bridge; the layout constants; per-message execution binding; the
collapsed 32px strip.

**Leave for later or skip:** pop-out window (`FLOATING`); canvas groups /
`GroupLogEntry`; sub-workflow recursion (AgentMesh has no sub-workflows);
partial execution per node (no backend support); token-streaming (see §2.4).

---

## Part 2 — Where AgentMesh stands today

### 2.1 The chat trigger already exists, and is served by a modal

`lib/data.ts:26` defines the trigger template `{ id: "chat", name: "On Chat
Message", desc: "Inbound chat" }`. `CanvasPage.tsx` detects it:

```tsx
const hasChatTrigger = useMemo(
  () =>
    workflow?.nodes.some(
      (n) => n.type === "trigger" && n.template === "chat",
    ) ?? false,
  [workflow],
);
```

and on Run, instead of executing, opens `ChatRunModal` (`CanvasPage.tsx:706`) —
a fixed-overlay dialog with a textarea. Submitting calls
`startRun({ message: msg })`, **closes the modal**, and opens the log drawer.

So today's flow is: _modal → type → modal vanishes → raw mono log rows._ There is
no conversation, no reply bubble, and no place the answer is presented **as an
answer**.

### 2.2 The answer exists in the data, but is buried in a debug affordance

`ExecuteAgent` returns the agent's final text as either a bare string or:

```go
return map[string]any{"message": content, "x402Payments": x402Payments}, nil
// or, on a platform key, platformKeyUsageResult(...) which adds
// "platformKeyUsage": { tier, model, tokensIn, tokensOut }
```

`LogDrawer`'s `OutputCell` then renders that payload — and because an agent answer
is routinely longer than the 200-character threshold, it collapses behind:

```
▸ response · 1.2 KB
```

**The single most important finding in this document:** the human-readable answer
is already computed, already streamed to the browser, and is currently displayed
as a collapsed byte-count toggle. The primary deliverable is not new data. It is
promoting data we already have into a view that treats it as prose.

### 2.3 `LogDrawer` is 834 lines and most of it is correctness we must not lose

`LogDrawer.tsx` already implements, with hard-won and well-commented reasons:

- **SSE deliberately bypassing the `/api` rewrite proxy** (lines 94–121) —
  Next's rewrite drops long-lived `text/event-stream` connections after ~30s,
  which silently reported "0/0 nodes succeeded" for runs that had really
  succeeded and moved money.
- **DB reconciliation polling** (lines 240–307) — the SSE broker has no replay, so
  events published before the client subscribes are lost forever. Polls
  `/runs/:id` up to 150 × 2s until the run reports a terminal status. A single
  post-stream fetch was tried and was not enough.
- **`es.onerror` hands off to polling instead of marking the run done** — treating
  a transient EventSource hiccup as completion previously reported success 3s into
  a 60s run and closed the stream.
- localStorage transcript cache with a 512KB cap; settlement recording to the
  usage page; drag-resize with window-level listeners; Tendril lease detection
  driving a Terminal tab.

**This code must be refactored, never rewritten.** Every one of those comments is
a postmortem. The plan below extracts them intact into a hook.

### 2.4 Backend constraints that shape the design

| Constraint                                        | Evidence                                                                                                      | Consequence                                                                                                    |
| ------------------------------------------------- | ------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------- |
| **No session / conversation / memory concept**    | No `sessionId`, `conversation`, or memory node anywhere in `internal/engine` or `internal/models`             | Multi-turn is not a UI task. See §3.6.                                                                         |
| **No token streaming**                            | `/runs/{runId}/stream` (`router.go:67`) emits per-node `log` events; `provider.go` collects whole completions | Replies appear when a node finishes, not token-by-token. No typing effect is possible without backend work.    |
| **`RunContext.input` is the raw trigger payload** | `UserInput()` returns `anyToString(rc.input)`                                                                 | Changing the run input shape to carry history would change what the agent is prompted with. Not a free change. |
| **Flat log events**                               | `LogEvent` has `stepIndex, nodeId, nodeType, status, output, durationMs, ts` — no parent link                 | The tree (§1.4) must be **derived on the client** from the workflow's `edges`, not received from the server.   |

---

## Part 3 — Design

### 3.1 The governing rule

> The chat pane must be **complete and sufficient on its own**. Everything
> technical is reachable from it in one click, and invisible until that click.

Concretely: a non-technical user must be able to run the workflow, read the answer,
and know it cost money — without ever seeing a `nodeId`, a JSON brace, or a
timestamp. A developer must reach the raw output of any step in one click from the
message that step contributed to.

### 3.2 Target layout

The bottom drawer stays where it is; its contents become two panes plus a details
pane. Collapsed height stays 32px — which already matches n8n exactly.

```
┌── Console ──────────────────────────────────── run · a3f1b2 · 12.4s ── ▾ ──┐
│ Chat                      ┆ Logs                    ┆ Details              │
│                           ┆                         ┆                      │
│           ┌─────────────┐ ┆ ▾ ● Research agent 8.1s ┆ tool402 · Serper     │
│           │ price of an │ ┆   ├ ○ gemini-2.0  6.2s  ┆ ──────────────────── │
│           │ ounce today?│ ┆   └ ◆ Serper     1.9s   ┆ INPUT                │
│           └─────────────┘ ┆ ▾ ● Format        0.1s  ┆ { "q": "gold spot" } │
│  ┌──────────────────────┐ ┆                         ┆ OUTPUT               │
│  │ Gold is trading at   │ ┆                         ┆ { "organic": [ … ] } │
│  │ about $2,640/oz…     │ ┆                         ┆ PAYMENT              │
│  └──────────────────────┘ ┆                         ┆ 0.004200 · in 7f2a…  │
│  2 tools · 8.2s · $0.0042 ┆                         ┆         · out 9c1b…  │
│                           ┆                         ┆                      │
│ [ Ask something…    ⏎ ]   ┆                         ┆                      │
└────────────────────────────────────────────────────────────────────────────┘
```

Default state for a workflow with a chat trigger: **Chat pane only, logs
collapsed.** The `Logs` toggle persists per user in `localStorage`. A developer
sets it once; a normal user never touches it. This is the mechanism that satisfies
"not important for normal people" — not hiding information, but changing which
audience has to opt in. Today the technical view is mandatory and the answer is
opt-in; we are inverting exactly that.

For a workflow with **no** chat trigger, the Chat pane does not render and the
console behaves as it does today. No regression for non-chat workflows.

### 3.3 The assistant bubble

Reply text resolution, in order:

1. Last successful `agent` node's output → `.message` if the output is an object,
   else the string itself.
2. If no agent node ran → last successful node's output, rendered as prose if
   string, else as a compact `<pre>`.
3. If the run failed → an error bubble carrying the failing node's name and error,
   with a `View logs →` action that opens the Logs pane with that row selected.

Under every assistant bubble sits one **activity strip** — the entire technical
summary compressed to a single clickable line:

```
2 tools · 8.2s · $0.0042
```

- Spend sums `x402Payments[].settledUsdMicros` across the run's log events —
  the same source `LogDrawer` already feeds to `recordSettlements`.
- The strip is `--fg-dim`, mono, ~10px — present but visually subordinate to the
  answer.
- Clicking it opens the Logs pane filtered to that message's `runId`. **This is
  the §1.5 selection bridge**, and it is the single most important interaction in
  the feature.

Render markdown in assistant bubbles — agents emit it constantly and raw `**bold**`
in a reply looks broken to exactly the non-technical audience this is for. Use a
minimal renderer (bold/italic/code/links/lists/fenced blocks); do not add a heavy
markdown dependency without checking bundle impact first.

### 3.4 The log tree, derived client-side

Since the backend sends flat events (§2.4), build the tree in the browser from the
workflow's own `edges`:

- A `tool` / `tool402` / `provider` node connected to an agent via an
  `attach`-kind edge (`e.kind === "attach"`, already used by
  `attachedSummaries` in `CanvasPage.tsx:179`) becomes a **child** of that agent's
  row.
- Everything else stays top-level, ordered by `stepIndex`.
- Aggregate duration and spend up the subtree; hide a parent's aggregate when it
  is expanded, matching n8n (§1.4).

This is a pure function over `(logs, workflow.edges)` and must be unit-tested — it
is the one piece of genuinely new logic with real edge cases (an agent that calls
the same tool twice, a tool attached to two agents, a log event whose node has
since been deleted from the canvas).

### 3.5 Component structure

Refactor, preserving `LogDrawer`'s hard-won behaviour verbatim:

```
components/canvas/
  ConsolePanel.tsx      (was LogDrawer) — shell: header, height resize, pane layout
  useRunTranscript.ts   — EXTRACTED VERBATIM from LogDrawer: SSE, reconcile
                          polling, onerror handoff, cache, settlements
  chat/
    ChatPane.tsx        — transcript, composer, activity strips
    ChatMessage.tsx     — one bubble + markdown + activity strip
    useChatSession.ts   — messages, per-workflow localStorage, session id
  logs/
    LogsOverview.tsx    — the tree (today's flat list, re-parented)
    LogRow.tsx          — one row: indent, status, name, duration, spend
    LogDetails.tsx      — input / output / payment panes
    buildLogTree.ts     — pure: (logs, edges) → LogEntry[]   ← unit-tested
```

`useRunTranscript.ts` is a **cut-and-paste extraction, comments included.** Any
behavioural change to SSE handling in this work is a bug, not an improvement.

### 3.6 Multi-turn conversation — the one open decision

The backend has no memory (§2.4). Three options:

|       | Approach                                              | Cost                        | Risk                                                                                                                                         |
| ----- | ----------------------------------------------------- | --------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------- |
| **A** | Client replays history into the run input             | Small frontend change       | Changes `RunContext.input`'s shape, so `UserInput()` changes what the agent is prompted with. Backend change with billing-path blast radius. |
| **B** | Each message is an independent run, threaded visually | None beyond the UI          | Agent has no recall across turns                                                                                                             |
| **C** | Real memory node + `sessionId` on runs                | Schema + engine + node work | Largest; correct long-term                                                                                                                   |

**Recommendation: ship B now, C later.** B is exactly n8n's model (§1.6) — every
message is a separate execution, and memory is an explicit opt-in feature there
too. It requires no backend change, so the panel ships behind zero coupling to
engine work.

**Stated assumption:** the rest of this plan assumes B. The chat pane will carry a
one-line, honest note in its empty state — _"Each message runs the workflow fresh;
agents don't remember previous messages yet"_ — rather than implying a continuity
that does not exist. If the devs want C in this piece of work, Phase 5 below is
where it lands and it roughly doubles the estimate.

---

## Part 4 — Build order and commit breakdown

Branch: `feature/workflow-chat-panel`, cut from an up-to-date `master`
(`upstream/master` — see `CLAUDE.md`).

Per the global preferences: atomic commits, Prettier over every touched
JS/TS/CSS/MD line, formatting-only changes in their own commit, no AI attribution,
no force push.

**Before writing any component**, load the `frontend-ui-taste` skill — it codifies
this app's dark-token design system, motion, and typography rules, and the panel is
a large new UI surface. Style with the app's own tokens (`--bg-elev-*`, `--fg-dim`,
`--font-mono`, `--r-2`), not with n8n's look.

### Phase 0 — Refactor, no behaviour change

| #   | Commit                                                            | Files                                      |
| --- | ----------------------------------------------------------------- | ------------------------------------------ |
| 0.1 | `canvas: extract run transcript state from LogDrawer into a hook` | new `useRunTranscript.ts`; `LogDrawer.tsx` |
| 0.2 | `canvas: rename LogDrawer to ConsolePanel`                        | `ConsolePanel.tsx`, `CanvasPage.tsx`       |

Gate: the console behaves **identically** — run a real workflow and confirm SSE
events, the reconciliation poll, the Terminal tab, and the cached last-run
transcript all still work. This is the riskiest phase precisely because it should
be invisible.

### Phase 1 — Chat pane replaces the modal

| #   | Commit                                                   | Files                                                        |
| --- | -------------------------------------------------------- | ------------------------------------------------------------ |
| 1.1 | `canvas: add chat session state, persisted per workflow` | `useChatSession.ts`                                          |
| 1.2 | `canvas: add chat pane with transcript and composer`     | `ChatPane.tsx`, `ChatMessage.tsx`                            |
| 1.3 | `canvas: resolve the agent's reply from run output`      | `resolveReply.ts` + test                                     |
| 1.4 | `canvas: run chat workflows from the panel, not a modal` | `CanvasPage.tsx` (delete `ChatRunModal`), `ConsolePanel.tsx` |

After 1.4 the feature is **already useful**: a non-technical user can type,
send, and read an answer. Everything after this is the developer half.

### Phase 2 — Logs become a tree

| #   | Commit                                                 | Files                              |
| --- | ------------------------------------------------------ | ---------------------------------- |
| 2.1 | `canvas: derive a log tree from workflow attach edges` | `buildLogTree.ts` + **unit tests** |
| 2.2 | `canvas: render logs as an expandable tree`            | `LogsOverview.tsx`, `LogRow.tsx`   |
| 2.3 | `canvas: show per-row duration and settled spend`      | `LogRow.tsx`                       |

### Phase 3 — The bridge

| #   | Commit                                                                 | Files                                  |
| --- | ---------------------------------------------------------------------- | -------------------------------------- |
| 3.1 | `canvas: add the log details pane with raw input, output and receipts` | `LogDetails.tsx`                       |
| 3.2 | `canvas: bind each chat message to the run it started`                 | `useChatSession.ts`, `ChatMessage.tsx` |
| 3.3 | `canvas: open filtered logs from a message's activity strip`           | `ConsolePanel.tsx`                     |
| 3.4 | `canvas: persist pane widths and the logs-visible preference`          | `panelSizing.ts`                       |

### Phase 4 — Polish

| #   | Commit                                                        | Files              |
| --- | ------------------------------------------------------------- | ------------------ |
| 4.1 | `canvas: render markdown in assistant messages`               | `ChatMessage.tsx`  |
| 4.2 | `canvas: add keyboard navigation to the logs tree`            | `LogsOverview.tsx` |
| 4.3 | `canvas: highlight the selected log row's node on the canvas` | `CanvasGraph.tsx`  |

### Phase 5 — Deferred, needs a separate decision

Multi-turn memory (§3.6 option C); response streaming; a `FLOATING` pop-out
window; human-in-the-loop approvals (n8n's Chat node, §1.7).

### Verification

Per the global preferences, UI changes are verified live in the running app, not
by compile success. For each phase:

1. `preview_start` the frontend dev server.
2. Open a workflow with a chat trigger; send a message; confirm the reply bubble
   renders the agent's prose and the activity strip shows plausible spend.
3. Confirm a workflow **without** a chat trigger is unchanged.
4. Confirm the reconciliation path still works — the surest test is a run with a
   real x402 settlement, since that is the >30s case the SSE comments describe.
5. Check `read_console_messages` for errors and take a screenshot for the PR.

### Risks

| Risk                                                   | Mitigation                                                                               |
| ------------------------------------------------------ | ---------------------------------------------------------------------------------------- |
| Phase 0 silently breaks SSE/reconciliation correctness | Extract verbatim with comments; no logic edits; verify against a real paid run           |
| Tree derivation wrong for reused/deleted nodes         | Pure function, unit-tested first (2.1 before 2.2)                                        |
| Chat implies memory it doesn't have                    | Honest empty-state copy (§3.6)                                                           |
| Three panes cramped on small screens                   | Canvas is already gated below the `md` breakpoint (`canvas-narrow`); below it, chat only |
| Markdown renderer bloats the bundle                    | Minimal hand-rolled renderer; measure before adding a dependency                         |

---

## Summary

n8n's chat is not a widget bolted onto a workflow editor — it is the human-facing
pane of a two-audience console, wired to the technical pane by selection. AgentMesh
already has the harder half of that: a battle-tested execution console, and an
agent reply that is already computed and already sent to the browser. It is
currently displayed as `▸ response · 1.2 KB`.

The work is to promote that string into a conversation, demote the log rows behind
an opt-in the developer sets once, and connect the two with a single clickable
activity line. Phase 1 alone delivers the devs' actual ask; Phases 2–4 are what
make it worth a coder's time.
