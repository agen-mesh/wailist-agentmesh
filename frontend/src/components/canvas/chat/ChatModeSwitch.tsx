"use client";

// Build and Run are two different conversations. The control that swaps
// between them used to be a single 9.5px pill labelled with the mode it was
// already in -- unreadable at a glance, and ambiguous when read. Same idiom
// as RailSwitch beside it: both labels visible, the active one under a thumb.

interface ChatModeSwitchProps {
  buildMode: boolean;
  /** Fires only when the mode actually changes. */
  onSelect: () => void;
  /** Without a chat trigger a run carries no message, so Run has no chat. */
  hasChatTrigger: boolean;
  /** False while the graph has nothing that could run yet. */
  canRun: boolean;
  /** A turn is in flight; switching now settles its reply against the other
   *  conversation, where the turn does not exist. */
  busy: boolean;
}

export function ChatModeSwitch({
  buildMode,
  onSelect,
  hasChatTrigger,
  canRun,
  busy,
}: ChatModeSwitchProps) {
  const runDisabled = busy || !hasChatTrigger || !canRun;
  const reason = busy
    ? "Wait for this turn to finish"
    : !hasChatTrigger
    ? "Add a Chat trigger to talk to this workflow \u2014 it starts from the Run button"
    : !canRun
      ? "Nothing to run yet"
      : undefined;

  return (
    <div
      className="rail-switch rail-switch--mode"
      data-segments="2"
      role="tablist"
      aria-label="Chat mode"
    >
      <span
        className="rail-switch__thumb"
        aria-hidden="true"
        style={{ transform: `translateX(${buildMode ? 0 : 100}%)` }}
      />
      <button
        type="button"
        role="tab"
        aria-selected={buildMode}
        className="rail-switch__seg"
        onClick={() => (buildMode || busy ? undefined : onSelect())}
        disabled={busy}
        title={busy ? "Wait for this turn to finish" : undefined}
      >
        Build
      </button>
      <button
        type="button"
        role="tab"
        aria-selected={!buildMode}
        className="rail-switch__seg"
        onClick={() => (buildMode && !runDisabled ? onSelect() : undefined)}
        disabled={runDisabled}
        title={runDisabled ? reason : undefined}
      >
        Run
      </button>
    </div>
  );
}
