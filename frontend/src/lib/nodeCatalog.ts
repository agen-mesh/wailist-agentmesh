// The node catalog: every node type, template and field the canvas really
// offers, in one plain object.
//
// It exists for the chat workflow builder. The builder used to work from a
// hand-typed list in its own prompt, which drifted: it offered a "cron"
// trigger that has never existed, knew 24 of the 42 connectors, could not
// create state or Google nodes at all, and had no idea which settings a node
// has -- so it built a JSON Extract step with no path and reported that it
// "extracts lastPrice". Everything here is assembled from what the canvas
// itself uses (the palette in data.ts, the connector tables in
// connectorFields.ts, the presets PalettePanel applies on drop), plus
// hand-written specs for the node types whose Inspector is bespoke JSX.
//
// The backend cannot import TypeScript, so this is serialised to
// backend/internal/engine/nodes/nodecatalog.json and embedded there.
// nodeCatalog.sync.test.ts fails when that file is stale; regenerate it with
// `npm run gen:node-catalog`. A Go test on the other side checks every
// template and key here against what the engine actually executes.

import {
  TRIGGER_TEMPLATES,
  AGENT_TEMPLATES,
  PROVIDER_TEMPLATES,
  TOOL_TEMPLATES,
  ACTION_TEMPLATES,
  GOOGLE_TEMPLATES,
  STATE_TEMPLATES,
  TENDRIL_TEMPLATES,
  END_TEMPLATES,
} from "./data";
import {
  CONNECTOR_CONFIG_FIELDS,
  CONNECTOR_AUTH,
  type ConnectorField,
} from "./connectorFields";

/**
 * Where a field's value lives on the saved node, which is also what decides
 * whether the builder may set it:
 * - field:      a top-level WorkflowNode property (url, stateKey, …)
 * - config:     node.config[key] — non-secret settings
 * - secret:     node.secrets[key] or an encrypted top-level key — a credential
 *               the builder never sees and must never invent; it names it for
 *               the user instead
 * - connection: an account the user links in the Inspector (e.g. Google OAuth)
 */
export type CatalogFieldWhere = "field" | "config" | "secret" | "connection";

export interface CatalogField {
  key: string;
  where: CatalogFieldWhere;
  label: string;
  hint?: string;
  placeholder?: string;
}

export interface CatalogTemplate {
  id: string;
  name: string;
  desc: string;
  /** A caveat the builder must respect, e.g. behaviour that is not implemented. */
  note?: string;
  /** Fields set automatically when the node is created, exactly as the palette does. */
  presets?: Record<string, string>;
  fields: CatalogField[];
  /** Where the user obtains this node's credential. */
  authDocUrl?: string;
  /** The connector can alternatively be linked with this OAuth provider. */
  oauthProvider?: string;
}

export interface CatalogType {
  type: string;
  desc: string;
  templates: CatalogTemplate[];
}

export interface NodeCatalog {
  version: 1;
  types: CatalogType[];
}

const MESSAGE_TEMPLATE: CatalogField = {
  key: "messageTemplate",
  where: "config",
  label: "Message",
  hint: "what this step sends; blank sends the previous step's output. {{ result }} / {{ result.field }} / {{ node.<id> }} / {{ state.key }} interpolate",
};

function fromConnectorTable(id: string): CatalogField[] {
  const spec = CONNECTOR_CONFIG_FIELDS[id];
  if (!spec) return [];
  return spec.fields.map((f: ConnectorField) => ({
    key: f.key,
    where: f.kind,
    label: f.label,
    ...(f.hint ? { hint: f.hint } : {}),
    ...(f.placeholder ? { placeholder: f.placeholder } : {}),
  }));
}

// Tool templates whose Inspector is not table-driven. The compute tools
// (set, json_extract, crypto, …) are table-driven and come from
// connectorFields.ts like the connectors do.
const TOOL_FIELDS: Record<string, CatalogField[]> = {
  http: [
    { key: "url", where: "field", label: "URL", placeholder: "https://api.example.com/v1/" },
    { key: "method", where: "field", label: "Method", placeholder: "GET" },
    {
      key: "httpBodyTemplate",
      where: "config",
      label: "Body template",
      hint: "only sent on POST/PUT/PATCH/DELETE -- {{ result }} or {{ result.field }}",
    },
    {
      key: "httpHeadersJSON",
      where: "secret",
      label: "Headers",
      hint: 'JSON object of header -> value, e.g. {"X-Api-Key": "…"}',
    },
    { key: "httpBasicUser", where: "secret", label: "Basic auth username" },
    { key: "httpBasicPass", where: "secret", label: "Basic auth password" },
  ],
  calc: [
    {
      key: "url",
      where: "field",
      label: "Expression",
      hint: "a math expression such as (2+3)*4 -- the calculator stores it in the url field",
    },
  ],
  websearch: [],
  xml: [],
};

const TOOL_NOTES: Record<string, string> = {
  websearch:
    "Answers with a live Google Search. Attached to an agent it searches whatever the agent asks; as a flow step it searches the previous step's output.",
  xml: "Parses the previous step's XML output into JSON. No settings.",
};

const GOOGLE_ACCOUNT: CatalogField = {
  key: "oauthCredentialID",
  where: "connection",
  label: "Google account",
  hint: "the user links a Google account in the Inspector",
};

const GOOGLE_FIELDS: Record<string, CatalogField[]> = {
  gmail_list: [
    { key: "gmailQuery", where: "config", label: "Query", hint: "Gmail search syntax, e.g. is:unread from:someone@x.com" },
    { key: "gmailMaxResults", where: "config", label: "Max results", placeholder: "10" },
  ],
  gmail_get: [
    { key: "gmailMessageID", where: "config", label: "Message ID", hint: "e.g. {{ result.id }} from an upstream Gmail: List step" },
  ],
  gmail_send: [
    { key: "gmailTo", where: "config", label: "To", placeholder: "recipient@example.com" },
    { key: "gmailSubject", where: "config", label: "Subject" },
  ],
  gmail_reply: [
    { key: "gmailTo", where: "config", label: "To", placeholder: "recipient@example.com" },
    { key: "gmailSubject", where: "config", label: "Subject" },
    { key: "gmailThreadID", where: "config", label: "Thread ID", hint: "keeps the reply in the original thread, e.g. {{ result.threadId }}" },
  ],
  sheets_read: [
    { key: "sheetsSpreadsheetID", where: "config", label: "Spreadsheet ID", hint: "the long id in the sheet's URL" },
    { key: "sheetsRange", where: "config", label: "Range", placeholder: "Sheet1!A1:Z1000" },
  ],
  sheets_append: [
    { key: "sheetsSpreadsheetID", where: "config", label: "Spreadsheet ID", hint: "the long id in the sheet's URL" },
    { key: "sheetsRange", where: "config", label: "Range", placeholder: "Sheet1!A1:Z1000" },
  ],
  calendar_list: [
    { key: "calendarID", where: "config", label: "Calendar ID", hint: "leave blank for the primary calendar", placeholder: "primary" },
  ],
  calendar_create: [
    { key: "calendarID", where: "config", label: "Calendar ID", hint: "leave blank for the primary calendar", placeholder: "primary" },
    { key: "calendarSummary", where: "config", label: "Title", hint: "leave blank to use the Message field" },
    { key: "calendarStart", where: "config", label: "Start", placeholder: "2026-08-10T10:00:00Z" },
    { key: "calendarEnd", where: "config", label: "End", placeholder: "2026-08-10T11:00:00Z" },
  ],
  drive_list: [
    { key: "driveQuery", where: "config", label: "Query", hint: "Drive search syntax, e.g. name contains 'report'" },
  ],
  drive_get: [
    { key: "driveFileID", where: "config", label: "File ID", hint: "e.g. {{ result.id }} from an upstream Drive: List step" },
  ],
  drive_download: [
    { key: "driveFileID", where: "config", label: "File ID", hint: "e.g. {{ result.id }} from an upstream Drive: List step" },
  ],
};

const EMAIL_FIELDS: CatalogField[] = [
  { key: "emailProvider", where: "field", label: "Provider", hint: "resend, postmark, sendgrid or brevo", placeholder: "resend" },
  { key: "emailFrom", where: "field", label: "From", hint: "must be verified with the provider", placeholder: "AgentMesh <you@yourdomain.com>" },
  { key: "emailTo", where: "field", label: "To", hint: "{{ variables }} supported", placeholder: "recipient@example.com" },
  { key: "emailSubject", where: "field", label: "Subject" },
  { key: "emailBody", where: "field", label: "Body", hint: "blank sends the previous step's output; {{ variables }} supported" },
  { key: "emailApiKey", where: "secret", label: "API key", hint: "the chosen email provider's API key" },
];

const TRIGGER_NOTES: Record<string, string> = {
  manual:
    "Started by the Run button -- and by the workflow's schedule, if one is set. Use this for anything that should run on a timetable.",
  chat: "Started by a message typed into the chat console; that message is the run's input.",
  webhook:
    "Started by an HTTP POST to the workflow's webhook URL, shown in the Inspector once deployed. The signing secret is generated automatically.",
};

const AGENT_NOTES: Record<string, string> = {
  router:
    "Runs exactly like a plain AI agent today -- the engine has no routing step. Do not promise the user classification-based dispatch.",
  human:
    "Runs exactly like a plain AI agent today -- the engine has no approval gate and never pauses for a human. Do not promise the user an approval step.",
};

export function buildNodeCatalog(): NodeCatalog {
  const types: CatalogType[] = [
    {
      type: "trigger",
      desc: "Where a run starts. Exactly one per workflow. There is no schedule/cron trigger: a timetable is a workflow-level setting (see the manual trigger).",
      templates: TRIGGER_TEMPLATES.map((t) => ({
        id: t.id,
        name: t.name,
        desc: t.desc,
        note: TRIGGER_NOTES[t.id],
        fields: [],
      })),
    },
    {
      type: "agent",
      desc: 'An LLM step. Needs a provider attached to its "model" port; tools attach to its "tools" port.',
      templates: AGENT_TEMPLATES.map((t) => ({
        id: t.id,
        name: t.name,
        desc: t.desc,
        ...(AGENT_NOTES[t.id] ? { note: AGENT_NOTES[t.id] } : {}),
        fields: [
          { key: "systemPrompt", where: "field" as const, label: "System prompt", hint: "instructions the agent follows on every run" },
        ],
      })),
    },
    {
      type: "provider",
      desc: 'The model behind an agent. Only ever attached to an agent\'s "model" port, never part of the flow.',
      templates: PROVIDER_TEMPLATES.map((t) => ({
        id: t.id,
        name: t.name,
        desc: `${t.name} models`,
        presets: { model: t.model },
        fields: [
          { key: "model", where: "field" as const, label: "Model", placeholder: t.model },
          { key: "keyMode", where: "field" as const, label: "Key mode", hint: '"platform" uses AgentMesh\'s key and bills credits (the default); "byok" uses the user\'s own key' },
          { key: "apiKey", where: "secret" as const, label: "API key", hint: 'only for keyMode "byok"' },
        ],
      })),
    },
    {
      type: "tool",
      desc: 'A deterministic step. Attach to an agent\'s "tools" port for the agent to call, or place directly in the flow.',
      templates: TOOL_TEMPLATES.map((t) => ({
        id: t.id,
        name: t.name,
        desc: t.desc,
        ...(TOOL_NOTES[t.id] ? { note: TOOL_NOTES[t.id] } : {}),
        fields: TOOL_FIELDS[t.id] ?? fromConnectorTable(t.id),
      })),
    },
    {
      type: "action",
      desc: "Sends something somewhere, or fetches from a service. Usually a flow step after the agent.",
      templates: ACTION_TEMPLATES.map((t) => {
        const auth = CONNECTOR_AUTH[t.id];
        const oauth = CONNECTOR_CONFIG_FIELDS[t.id]?.oauthProvider;
        return {
          id: t.id,
          name: t.name,
          desc: t.desc,
          fields: t.id === "email" ? EMAIL_FIELDS : [...fromConnectorTable(t.id), MESSAGE_TEMPLATE],
          ...(auth ? { authDocUrl: auth.docUrl } : {}),
          ...(oauth ? { oauthProvider: oauth } : {}),
        };
      }),
    },
    {
      type: "google",
      desc: "Gmail, Sheets, Calendar and Drive, through one Google account the user links in the Inspector.",
      templates: GOOGLE_TEMPLATES.map((t) => ({
        id: t.id,
        name: t.name,
        desc: t.desc,
        fields: [
          GOOGLE_ACCOUNT,
          ...(GOOGLE_FIELDS[t.id] ?? []),
          ...("usesMessage" in t && t.usesMessage ? [MESSAGE_TEMPLATE] : []),
        ],
      })),
    },
    {
      type: "state",
      desc: "Values that persist between runs of this workflow (a cursor, a counter, the last price seen). Read anywhere as {{state.key}}.",
      templates: STATE_TEMPLATES.map((t) => ({
        id: t.id,
        name: t.name,
        desc: t.desc,
        presets: { stateOp: t.id },
        fields: [
          { key: "stateKey", where: "field" as const, label: "Key", hint: "persists across runs", placeholder: "lastRowId" },
          ...(t.id === "set"
            ? [{ key: "stateValue", where: "field" as const, label: "Value", hint: "blank stores the previous step's output" }]
            : t.id === "increment"
              ? [{ key: "stateValue", where: "field" as const, label: "Amount", hint: "defaults to 1" }]
              : []),
        ],
      })),
    },
    {
      type: "tendril",
      desc: "Rented Tendril compute: buy credit, rent a machine, run code on it, release it.",
      templates: TENDRIL_TEMPLATES.map((t) => ({
        id: t.id,
        name: t.name,
        desc: t.desc,
        presets: { tendrilAction: t.action, tendrilHours: "1", tendrilAmount: "10" },
        fields:
          t.action === "topup"
            ? [{ key: "tendrilAmount", where: "field" as const, label: "Amount (USD)", placeholder: "10" }]
            : t.action === "rent"
              ? [{ key: "tendrilHours", where: "field" as const, label: "Hours", placeholder: "1" }]
              : [],
      })),
    },
    {
      type: "end",
      desc: "Where the run finishes.",
      templates: END_TEMPLATES.map((t) => ({
        id: t.id,
        name: t.name,
        desc: t.desc,
        // "Respond to Webhook" reads as if the webhook caller gets this
        // output back. It does not: the public trigger answers 202 {runId}
        // before the run even finishes (handlers/runs.go PublicTrigger).
        note: "Finishes the run and passes the last output through; the http and done templates behave identically. A webhook caller never receives run output -- the webhook answers 202 with a runId immediately -- so do not promise a response body.",
        fields: [],
      })),
    },
  ];
  return { version: 1, types };
}
