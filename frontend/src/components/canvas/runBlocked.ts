// Why a run cannot start, in the user's terms.
//
// Both entry points (the Run button and a message typed into the console) used
// to say "Deploy first to run" for every un-deployed workflow. That is only
// sometimes the real obstacle. A workflow with no provider node has nothing to
// deploy YET -- telling someone to deploy is a dead end, because the button
// they are being pointed at will not help them. And a read-only viewer cannot
// deploy at all, so naming deployment as the next step is advice they cannot
// take.
//
// Pure and framework-free so the whole table is unit-testable, which matters
// here: this is messaging logic, and messaging is exactly the kind of thing
// that rots silently.

export interface RunBlockedInput {
  /** Has the workflow been deployed? */
  deployed: boolean;
  /** Does the graph contain a provider node -- i.e. is there anything to run? */
  hasProviderNode: boolean;
  /** May THIS client deploy? False for a read-only viewer. */
  canDeploy: boolean;
}

/**
 * What the card shows. `title` is the headline (what is wrong), `detail` is
 * the sentence under it (what to do about it), and `action` is the button to
 * render -- null when there is no button this client could usefully press.
 *
 * Split out from the single string the toast used to take because a string
 * cannot carry a button: "Deploy first to run" flashed for 2.4 seconds and
 * pointed at a control the user then had to go find.
 */
export interface RunBlockedReason {
  code: "no-provider" | "not-deployed";
  title: string;
  detail: string;
  action: "deploy" | null;
}

export function runBlockedReason({
  deployed,
  hasProviderNode,
  canDeploy,
}: RunBlockedInput): RunBlockedReason | null {
  if (deployed) return null;

  // Nothing to deploy yet. Say what is actually missing rather than pointing at
  // a Deploy button that would not help -- and offer no action, because there
  // is no single click that fixes this.
  if (!hasProviderNode) {
    return {
      code: "no-provider",
      title: "No model attached yet",
      detail: canDeploy
        ? "Add a provider node before running"
        : "This workflow has no agent yet — build it in the AgentMesh desktop app",
      action: null,
    };
  }

  // Deployable, but not deployed. Only offer the button to someone who has it.
  return {
    code: "not-deployed",
    title: "Not deployed",
    detail: canDeploy
      ? "Deploy first to run"
      : "Not deployed yet — deploy it from the AgentMesh desktop app",
    action: canDeploy ? "deploy" : null,
  };
}

// Returns the message to show, or null when nothing is blocking the run.
// Retained for callers that only need the sentence. Derived from
// runBlockedReason so the two can never disagree about what is wrong.
export function runBlockedMessage(input: RunBlockedInput): string | null {
  return runBlockedReason(input)?.detail ?? null;
}
