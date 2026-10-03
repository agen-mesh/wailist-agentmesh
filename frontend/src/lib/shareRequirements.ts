import type { ShareImportRequirements } from "./types";

// What a recipient still has to supply, said in sentences.
//
// Plain sentences, not a table of counts: somebody is deciding whether to
// import, not auditing a diff, and "your own API key for one model node" is
// the useful form of "apiKeys: 1".
//
// Lives here rather than in the preview page because two surfaces ask the
// same question now -- the public page behind a link, and the Import dialog,
// which shows what a pasted link holds before it creates anything. Two copies
// of this wording would drift, and the drift would be invisible: both would
// go on rendering a plausible sentence.

export function requirementLines(req: ShareImportRequirements): string[] {
  const lines: string[] = [];
  if (req.apiKeys > 0) {
    lines.push(
      req.apiKeys === 1
        ? "Your own API key, for one model node"
        : `Your own API keys, for ${req.apiKeys} model nodes`,
    );
  }
  if (req.connectedAccounts > 0) {
    lines.push(
      req.connectedAccounts === 1
        ? "Your own Google account, connected once"
        : `Your own Google account, for ${req.connectedAccounts} nodes`,
    );
  }
  // Named, not counted. "Reconnect Slack and Jira" is something a person can
  // go and do; "2 connectors" is a number they then have to hunt for. The
  // sender's tokens were stripped, so every connector in the graph needs the
  // recipient's own account -- and before this existed the page cheerfully
  // told somebody importing a Slack workflow it was "ready to run as-is".
  const connectors = req.connectors ?? [];
  if (connectors.length > 0) {
    lines.push(
      `Your own ${listSentence(connectors.map(titleCase))} ${
        connectors.length === 1 ? "account" : "accounts"
      }, reconnected once`,
    );
  }
  if (req.files > 0) {
    lines.push(
      req.files === 1
        ? "A file to upload, where the original had one"
        : `${req.files} files to upload, where the original had them`,
    );
  }
  return lines;
}

// Provider names arrive as the graph spells them -- "slack", "gitlab" -- which
// is a template id, not a brand. Capitalising the first letter is as far as
// this goes on purpose: a table of proper spellings ("GitLab", "HubSpot")
// would be one more list to keep in step with the connector registry, and
// letting that go subtly stale reads worse than a plain capital.
function titleCase(name: string): string {
  return name.charAt(0).toUpperCase() + name.slice(1);
}

function listSentence(items: string[]): string {
  if (items.length <= 1) return items[0] ?? "";
  if (items.length === 2) return `${items[0]} and ${items[1]}`;
  return `${items.slice(0, -1).join(", ")} and ${items[items.length - 1]}`;
}
