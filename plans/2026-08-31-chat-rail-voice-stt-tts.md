# Plan — Chat rail voice input (STT) and response playback (TTS)

Status: reviewed, ready to implement.
Branch base: `master` @ `ed8d69d`.
Closes: #108.

## Brainstorm / decisions

Issue #108 asks for two independent, additive features on the run chat rail:

1. A mic button in the composer that dictates into the draft textbox before send.
2. A speaker button on each assistant reply that reads it aloud.

Both are specced to use the browser-native Web Speech API
(`SpeechRecognition`/`webkitSpeechRecognition` for input, `speechSynthesis` for output) rather
than a hosted provider — zero new dependencies, consistent with how this codebase already
prefers hand-rolled solutions over packages for things the browser provides (inline SVG icons
instead of an icon library, no UI kit at all). `frontend/package.json` has no speech/audio deps
today and none are proposed here.

**Correction to the issue text:** there is no `CLAUDE.md` in this repo (checked the whole tree).
The issue's "per CLAUDE.md" pointer doesn't resolve to a real file — the actual chat rail lives
at `frontend/src/components/canvas/chat/`, confirmed by reading the code directly, and that's
what this plan cites throughout.

**Correction to the issue's dependency on #107:** #107 ("markdown/code/LaTeX rendering") is not
implemented. `ChatMessage.tsx` renders `message.text` as raw plain text (`whiteSpace: pre-wrap`,
no parser, no `remark`/`katex`/`react-markdown` anywhere in `frontend/`). So there is no markdown
AST to strip for TTS — this plan writes its own lightweight plain-text normalizer instead of
piggy-backing on a renderer, since blocking #108 on #107 landing first isn't necessary and the
issue's own suggested approach ("strip/normalize") doesn't require a real parser, just a decent
regex-based cleanup. If #107 lands later with a real markdown AST, the normalizer can be swapped
for an AST-to-text pass without touching anything else in this plan.

Key implementation calls, each with the reasoning:

- **Mic session shape:** one-shot (`recognition.continuous = false`), not continuous listening.
  Continuous mode's auto-stop-on-silence behavior differs enough across Chrome/Safari to be
  unpredictable, and correctness matters more than saving a second tap. The user taps to start,
  speaks, and the session ends itself (or they tap again to stop early); tapping again after
  that starts a new session and appends to the existing draft rather than replacing it, so
  multiple dictation passes can build one message.
- **Interim results shown live:** `recognition.interimResults = true` so text appears in the
  textarea as it's recognized rather than only on completion — the standard dictation UX and
  cheap to add. Interim text is visually distinguished (dimmed) from committed/final text isn't
  practical with a plain `<textarea>` (no rich inline styling), so interim results are shown as
  plain draft text and simply get overwritten/finalized in place; this matches what a plain
  `<input>`-based dictation UI does elsewhere and needs no new UI primitive.
- **No mic UI where unsupported.** Firefox has no `SpeechRecognition` implementation as of this
  writing. Feature-detect (`window.SpeechRecognition ?? window.webkitSpeechRecognition`) once at
  module load and simply omit the mic button when absent — no error state needed for "your
  browser doesn't support this," only for "you have the feature but denied/blocked it."
- **Permission errors surface inline, next to the composer** — a one-line dismissible message
  ("Microphone access was blocked — allow it in your browser's site settings.") triggered by
  `recognition.onerror` with `error === "not-allowed" | "service-not-allowed"`. No modal, no
  toast (the existing `Toast` component is positioned for app-level events, not per-field
  errors) — matches how nothing else in this rail uses toasts for local validation.
- **TTS is a singleton across the transcript.** The Web Speech API can only speak one utterance
  at a time; without coordination, clicking a second message's speaker button while the first is
  still playing would either queue (awkward) or silently do nothing depending on browser. A
  small module-scope coordinator (`speechPlayback.ts`) owns "which message id is currently
  speaking," calls `speechSynthesis.cancel()` before starting a new utterance, and every
  `ChatMessage`'s speaker button subscribes to it so only the currently-speaking message shows
  the "stop" state — the rest fall back to "play" automatically when interrupted. This avoids
  prop-drilling playback state through `ChatPane`/`ChatRail`/`CanvasPage`, none of which need to
  know about it.
- **Chrome's long-utterance cutoff.** Chrome has a long-standing bug where `speechSynthesis`
  silently stops mid-utterance past roughly 15s on some platforms unless paused/resumed
  periodically. Mitigate with the well-known workaround: while an utterance is active, call
  `speechSynthesis.pause(); speechSynthesis.resume();` on a ~10s interval, cleared on
  `onend`/`onerror`. Cheap, no dependency, and only active while actually speaking.
- **Cleanup on navigation.** `ChatPane` is always mounted while its `ChatRail` tab exists
  (`ChatRail.tsx:151-165`, kept mounted so draft state survives a tab switch) but the whole rail
  unmounts when the user leaves the workflow/canvas page. A `ChatPane` unmount effect calls
  `speechSynthesis.cancel()` so switching workflows doesn't leave an old reply talking over the
  new page. Recognition sessions are already one-shot and self-terminate; no equivalent cleanup
  needed there, but the hook still cancels any in-flight session on unmount for safety.
- **Stop dictation on send.** `ChatPane`'s `submit()` (lines 30-35) clears `draft` to `""` and
  sends. Without an explicit stop, a `SpeechRecognition` session left running past a Send tap
  would deliver its next `onresult` into the now-empty `draft` after the message is already
  gone — the transcribed tail reappears in the box with nothing to attach it to. `submit()` must
  call the STT hook's `stop()` before clearing `draft`, so any in-flight session is torn down as
  part of sending. (Found in review; not in the original issue text, but a direct consequence of
  the composer's existing clear-on-submit behavior once dictation shares the same `draft`.)
- **No new TypeScript deps for `SpeechRecognition` types.** `speechSynthesis`/
  `SpeechSynthesisUtterance` are already in `lib.dom.d.ts` (`tsconfig.json` has `"lib": ["dom",
  ...]`), so TTS needs no ambient types. `SpeechRecognition` is *not* in the standard DOM lib —
  declare a minimal ambient interface (just the members actually used: `continuous`,
  `interimResults`, `lang`, `start`, `stop`, `onresult`, `onerror`, `onend`, plus the result event
  shapes) rather than pulling in a `@types/dom-speech-recognition` package, since only a handful
  of members are touched. Put it in its own `frontend/src/types/speech-recognition.d.ts`, matching
  the repo's one existing ambient-type precedent (`frontend/src/types/cashfree-js.d.ts`), instead
  of inlining it in the hook file.
- **Accepted edge case: `busy` flipping true mid-dictation.** The mic button is disabled by the
  same `busy` condition the textarea already uses (lines 132-133), which blocks *starting* a new
  recording while a run is in flight, but doesn't stop a session already recording if `busy`
  flips true underneath it (e.g. a stranded turn recovering from a reload). Low-probability and
  self-resolving (the session still ends and appends to `draft` normally, it's just not sendable
  until `busy` clears) — left unhandled deliberately rather than adding a `busy`-triggered
  `stop()` for a case this narrow.

## Files

New (all under `frontend/src/components/canvas/chat/`, matching the existing pattern of pulling
logic out of components into small `.ts`/`.ts` hook modules with co-located tests — see
`resolveReply.ts`/`resolveReply.test.ts`):

- **`speechText.ts`** — pure function `toSpeechText(raw: string): string`. Strips, in order:
  fenced code blocks (```...``` → replaced with a short spoken placeholder, e.g. `"(code
  omitted)"` — reading raw code aloud is unusable), inline code (`` `x` `` → `x`, backticks
  dropped but content kept), LaTeX (`$$...$$`, `$...$`, `\(...\)`, `\[...\]` → replaced with
  `"(equation)"` — same reasoning as code blocks), markdown links/images (`[text](url)` → `text`,
  `![alt](url)` → `alt`), heading/emphasis/strikethrough/blockquote/list-marker syntax characters
  (`#`, `**`, `__`, `*`, `_`, `~~`, leading `>`, leading `-`/`*`/`+`/`1.`), horizontal rules, and
  collapses runs of whitespace/blank lines into single spaces/single newlines. Pure, no DOM — the
  only genuinely unit-testable surface of this feature, so it gets real test coverage.
- **`speechText.test.ts`** — vitest, same style as `resolveReply.test.ts`: cases for fenced code,
  inline code, bold/italic, links, headers, lists, LaTeX, and a "plain sentence passes through
  unchanged" baseline.
- **`useSpeechRecognition.ts`** — hook wrapping `SpeechRecognition`, typed against the ambient
  `SpeechRecognition`/`SpeechRecognitionEvent`/`SpeechRecognitionErrorEvent` interfaces declared
  in `frontend/src/types/speech-recognition.d.ts` (new — see Files/edited below). Returns
  `{ supported, listening, error, start, stop }`; takes an
  `onResult: (finalText: string) => void` callback the caller uses to append into its draft
  state. Internals: construct one recognition instance per `start()` call (Web Speech
  `SpeechRecognition` objects are typically single-use per session in practice), `continuous =
  false`, `interimResults = true`, `lang = navigator.language`; `onresult` walks
  `event.results` from `event.resultIndex`, calling `onResult` for the final result text and
  optionally surfacing interim text via a second returned field (`interimText`) if wiring it into
  the textarea turns out cleaner than mutating draft directly — decide at implementation time
  based on how `ChatPane`'s `draft` state composes with live interim updates without fighting the
  user's own typing.
- **`speechPlayback.ts`** — module-scope singleton coordinator described above:
  `speak(id: string, text: string): void`, `stopIfSpeaking(id: string): void` (or a general
  `stop()`), `subscribe(id: string, cb: () => void): () => void` (returns unsubscribe), and
  `isSpeaking(id: string): boolean`. Owns the Chrome pause/resume keep-alive interval internally.
  No test file — this is coordination glue over a browser singleton, not pure logic; same
  boundary the codebase already draws (no tests for `ChatMessage.tsx`/`ChatPane.tsx` themselves).
- **`useSpeechPlayback.ts`** — thin hook wrapping `speechPlayback.ts`'s subscribe/speak/stop for
  one message id, used by `ChatMessage`. (Or fold directly into `ChatMessage.tsx` if the hook
  ends up trivial enough not to earn its own file — decide during implementation once the actual
  line count is visible.)

New (elsewhere):

- **`frontend/src/types/speech-recognition.d.ts`** — ambient `SpeechRecognition` /
  `SpeechRecognitionEvent` / `SpeechRecognitionErrorEvent` interfaces and the
  `Window.SpeechRecognition`/`Window.webkitSpeechRecognition` augmentation, following the
  existing `frontend/src/types/cashfree-js.d.ts` precedent rather than a package.

Edited:

- **`frontend/src/components/ui/index.tsx`** — add `IconMic` (mic body + stand, following the
  existing hand-drawn-SVG pattern at lines 238-380, `stroke="currentColor"`,
  `size`-parameterized) and `IconSpeaker` (speaker body + sound-wave arcs). Reuse the existing
  `IconStop` (lines 257-261) for the "currently speaking / tap to stop" state rather than adding
  a third icon.
- **`frontend/src/components/canvas/chat/ChatPane.tsx`** — composer block (lines 108-179). Add a
  mic `<button>` between the textarea and the Send button (or to the textarea's left — decide by
  what reads better once both are in place; the Send button's `44px` touch-target treatment,
  lines 160-175, is the sizing precedent to match). Wire `useSpeechRecognition`'s `onResult` to
  append into `draft` (`setDraft((prev) => prev + (prev.trim() ? " " : "") + finalText)`), gate
  the button's presence on `supported`, show a recording indicator (new `.chat-mic--recording`
  class reusing the `pulse` keyframe, mirroring `.chat-thinking-dot`) while `listening`, and
  render the inline permission-error line when `error` is set. Disable the mic button under the
  same `busy` condition the textarea already uses. `submit()` (lines 30-35) also calls the STT
  hook's `stop()` before `setDraft("")`, so a Send tap during dictation can't have a stray final
  result land in the box after the message is already gone (the fix from plan review).
- **`frontend/src/components/canvas/chat/ChatMessage.tsx`** — assistant branch (lines 65-153).
  Add a speaker `<button>` as a sibling to the existing activity-strip button (lines 127-151),
  gated on `!message.pending && message.text.trim() !== ""`. On click, call
  `toSpeechText(message.text)` then hand it to `useSpeechPlayback(message.id)`'s `speak`/`stop`.
  Icon swaps between `IconSpeaker` (idle) and `IconStop` (this message is the one speaking),
  driven by the hook's subscribed `isSpeaking` boolean — every other message's button reverts to
  idle automatically when a different one starts, with no prop plumbing required.
- **`frontend/src/app/globals.css`** — extend the "Workflow console chat" section (lines
  949-1015): `.chat-mic` base + hover/active/focus-visible states matching `.chat-activity`'s
  transition style (lines 968-981), `.chat-mic--recording` reusing `pulse 1.2s var(--ease)
  infinite` exactly like `.chat-thinking-dot` (lines 958-964), and a `.chat-speak` class for the
  new speaker button with the same hover/active/focus-visible treatment as `.chat-activity`. Add
  both new animated classes to the existing `prefers-reduced-motion` block (lines 1000-1015) so
  the recording pulse and any transform-on-active are disabled there too, matching the rest of
  the section.

No backend changes. No new dependencies. No changes to `useChatSession.ts`'s `ChatMessage`
interface — `id` and `text` are already there and are all the new features need.

## Milestones

Each is independently mergeable and testable; there's no hard ordering dependency between M2 and
M3, but M1 lands first since M2 depends on it.

### M1 — `speechText.ts` normalizer + tests

Self-contained pure function, zero UI. Ship first: it's the one piece with real unit-test
coverage and needs no browser to verify.

### M2 — TTS playback on assistant messages

`speechPlayback.ts`, `useSpeechPlayback.ts` (or inlined), `IconSpeaker`/`IconStop` wiring,
`ChatMessage.tsx` button, `.chat-speak` CSS. Depends on M1 for the text fed to
`speechSynthesis.speak`.

### M3 — STT mic input in the composer

`frontend/src/types/speech-recognition.d.ts`, `useSpeechRecognition.ts`, `IconMic`,
`ChatPane.tsx` button + recording state + permission-error line + the `submit()` stop-on-send
fix, `.chat-mic`/`.chat-mic--recording` CSS. Independent of M1/M2.

## Verification

```
cd frontend && pnpm typecheck && pnpm lint && pnpm test && pnpm build
```

`pnpm test` covers `speechText.test.ts`. The rest is unverifiable by the existing test suite (no
component-render tests exist for this directory today, per the established convention) and needs
a real browser pass:

- Chromium (pre-installed in this environment): click the mic button, grant permission, speak,
  confirm interim text appears and final text lands in the draft without being sent; deny
  permission and confirm the inline error appears; click a speaker button and confirm playback
  starts and the icon flips to "stop"; click a second message's speaker button mid-playback and
  confirm the first stops and its icon reverts; test a message containing fenced code and inline
  math to confirm it isn't read as raw markdown/LaTeX syntax; verify a long (~30s+) reply doesn't
  cut off partway through (Chrome keep-alive workaround); check `prefers-reduced-motion` disables
  the recording pulse.
- A browser without `SpeechRecognition` (simulate by stubbing `window.SpeechRecognition` /
  `webkitSpeechRecognition` to `undefined`, or note Firefox if available): confirm the mic button
  is simply absent and nothing else in the composer breaks; `speechSynthesis` playback should
  still work there since it's far more broadly supported.
- Switch workflows (or navigate off the canvas) while a reply is speaking; confirm playback stops
  rather than continuing over the new page.

## Open questions

- **Interim-text UX**: does live interim text write directly into `ChatPane`'s `draft` state (so
  it's visible and editable as it streams), or does the composer show interim text in a separate
  overlay until finalized? Decide during M3 implementation once both are tried — the plan assumes
  the simpler "write into `draft` directly" approach but flags this as the one place where the
  actual UX may need a look before locking in.
- **Placement of the mic button** relative to the textarea/Send button — left of the textarea,
  or between the textarea and Send — is a visual call best made with the real composer in front
  of you rather than decided on paper.
- Should there be a per-message rate/voice preference, or a global mute? Out of scope for this
  pass per the issue's "zero-dependency first pass" framing; flag as a natural follow-up if
  requested later.

## Review log

Reviewed against the live repo before implementation: every file/line citation re-checked
(composer, message render, rail mount, `CanvasPage.tsx:665-667` wiring, `globals.css` chat
section, `vitest.config.ts`'s `.test.ts`-only include, `IconStop`'s existing use at
`CanvasPage.tsx:877`, absence of any existing speech-related code/types/deps). One real
correctness gap found and fixed above: `submit()` didn't stop an in-flight dictation session,
which would have leaked a stray transcript into the box after a message was already sent. Two
nits folded in: the ambient `SpeechRecognition` types moved to their own `frontend/src/types/`
file to match the repo's existing convention, and the `busy`-mid-dictation edge case is now
explicitly called out as an accepted, deliberately-unhandled risk rather than a silent gap.
