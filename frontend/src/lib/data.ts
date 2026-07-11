import { NodeTypeMeta, Workflow } from "./types";

export const NODE_TYPES: Record<string, NodeTypeMeta> = {
  trigger:  { w: 200, h: 60,  ports: ["out"] },
  agent:    { w: 260, h: 124, ports: ["in", "out", "model", "tools"] },
  provider: { w: 220, h: 76,  ports: ["top"] },
  tool:     { w: 200, h: 64,  ports: ["top"] },
  tool402:  { w: 220, h: 84,  ports: ["top"] },
  action:   { w: 200, h: 64,  ports: ["in", "out"] },
  end:      { w: 200, h: 60,  ports: ["in"] },
};

export const TRIGGER_TEMPLATES = [
  { id: "manual",  name: "Manual Trigger",    desc: "Click to test",      icon: "▶" },
  { id: "chat",    name: "On Chat Message",   desc: "Inbound chat",       icon: "◴" },
  { id: "webhook", name: "Webhook",           desc: "HTTP POST endpoint", icon: "◷" },
  { id: "cron",    name: "Schedule",          desc: "Cron / interval",    icon: "◵" },
];

export const AGENT_TEMPLATES = [
  { id: "agent",  name: "AI Agent",       desc: "Reasoning + tool use", icon: "◇" },
  { id: "router", name: "Router Agent",   desc: "Classify + dispatch",  icon: "◊" },
  { id: "human",  name: "Human-in-loop",  desc: "Approval gate",        icon: "○" },
];

export const PROVIDER_TEMPLATES = [
  { id: "gemini",    name: "Google Gemini", model: "gemini-2.5-flash",  icon: "G" },
  { id: "openai",    name: "OpenAI",        model: "gpt-4.1",           icon: "O" },
  { id: "anthropic", name: "Anthropic",     model: "claude-sonnet-4",   icon: "A" },
  { id: "mistral",   name: "Mistral",       model: "mistral-large",     icon: "M" },
  { id: "groq",      name: "Groq",          model: "llama-3.3-70b",     icon: "q" },
];

export const TOOL_TEMPLATES = [
  { id: "http",   name: "HTTP Request",       desc: "GET/POST any URL",      icon: "⟶" },
  { id: "code",   name: "Code",               desc: "Run JS/Python inline",  icon: "{}" },
  { id: "calc",   name: "Calculator",         desc: "Math expressions",      icon: "Σ" },
  { id: "vector", name: "Vector Store",       desc: "Pinecone / pgvector",   icon: "⊕" },
  { id: "memory", name: "Conversation Memory",desc: "Recent turns",          icon: "◐" },
];

export const TOOL402_TEMPLATES = [
  { id: "tavily",    name: "Tavily Search",    provider: "tavily.x402",     price: "0.002", unit: "call",  icon: "⌕" },
  { id: "firecrawl", name: "Firecrawl Scrape", provider: "firecrawl.x402",  price: "0.005", unit: "page",  icon: "◐" },
  { id: "alpaca",    name: "AlpacaQuote",      provider: "alpaca.x402",     price: "0.001", unit: "quote", icon: "$" },
  { id: "ocr",       name: "OCR.space",        provider: "ocr.x402",        price: "0.003", unit: "page",  icon: "⊟" },
  { id: "flux",      name: "FluxImage",        provider: "flux.x402",       price: "0.020", unit: "image", icon: "✦" },
  { id: "weather",   name: "WeatherKit",       provider: "weatherkit.x402", price: "0.0008",unit: "call",  icon: "◌" },
];

export const ACTION_TEMPLATES = [
  { id: "email",   name: "Send Email",     desc: "Postmark / Resend", icon: "✉" },
  { id: "slack",   name: "Slack Message",  desc: "Post to channel",   icon: "#" },
  { id: "db",      name: "Database Insert",desc: "Postgres / Neon",   icon: "▤" },
  { id: "discord", name: "Discord Message",desc: "Webhook post",      icon: "d" },
  { id: "teams",   name: "Teams Message",  desc: "Webhook post",      icon: "T" },
  { id: "google_chat", name: "Google Chat Message", desc: "Webhook post", icon: "G" },
  { id: "ntfy", name: "Ntfy Push", desc: "Topic notification", icon: "n" },
  { id: "telegram", name: "Telegram Message", desc: "Bot API send", icon: "t" },
  { id: "github", name: "GitHub Issue", desc: "Create an issue", icon: "gh" },
];

export const END_TEMPLATES = [
  { id: "http", name: "Respond to Webhook", desc: "Return JSON",   icon: "◳" },
  { id: "done", name: "End",                desc: "Mark complete", icon: "■" },
];

export const SAMPLE_WORKFLOW: Workflow = {
  id: "wf-weather",
  name: "Weather Agent Test",
  nodes: [
    { id: "n1", type: "trigger",  template: "chat",   x: 80,  y: 220,
      label: "Chat trigger" },
    { id: "n2", type: "agent",    template: "agent",  x: 380, y: 200,
      name: "Weather Agent",
      systemPrompt: "You receive a message from the user. Use the x402 weather tool to get current weather for any city they mention, then return a clear, friendly summary of the conditions. If no city is mentioned, ask the user which city they want." },
    { id: "n3", type: "provider", template: "gemini", x: 300, y: 430,
      name: "Gemini 2.5 Flash", model: "gemini-2.5-flash" },
    { id: "n4", type: "tool402",  custom: true,       x: 500, y: 430,
      name: "x402 Weather",
      description: "Real-time weather data — temperature, wind, conditions for any city worldwide. Accepts: city (string, required), units (celsius|fahrenheit, optional).",
      endpoint: "http://localhost:4402/weather",
      price: "0.065", unit: "call", priceLive: true,
      discoveredParams: [
        { name: "city",  type: "string", required: true,  description: "City name (e.g. London, Tokyo)" },
        { name: "units", type: "string", required: false, description: "celsius or fahrenheit", default: "celsius" },
      ],
    },
    { id: "n5", type: "action",   template: "email",  x: 700, y: 200,
      name: "Send Result Email" },
    { id: "n6", type: "end",      template: "done",   x: 960, y: 210 },
  ],
  edges: [
    { id: "e1", from: "n1", to: "n2", kind: "flow",   toPort: "in" },
    { id: "e2", from: "n3", to: "n2", kind: "attach", toPort: "model" },
    { id: "e3", from: "n4", to: "n2", kind: "attach", toPort: "tools" },
    { id: "e4", from: "n2", to: "n5", kind: "flow",   toPort: "in" },
    { id: "e5", from: "n5", to: "n6", kind: "flow",   toPort: "in" },
  ],
};

export const WORKFLOWS: Workflow[] = [
  { id: "wf-triage",  name: "Customer Support Triage",   status: "active", updated: "2m ago",    agents: 1, runs: 1842, spend: "4.218", tags: ["support", "production"],  nodes: [], edges: [] },
  { id: "wf-brief",   name: "Daily Market Brief",         status: "active", updated: "1h ago",    agents: 4, runs: 38,   spend: "1.482", tags: ["research"],                nodes: [], edges: [] },
  { id: "wf-invoice", name: "Invoice Reconciliation",     status: "paused", updated: "yesterday", agents: 2, runs: 217,  spend: "0.890", tags: ["finance"],                 nodes: [], edges: [] },
  { id: "wf-leads",   name: "Lead Enrichment v2",         status: "draft",  updated: "3d ago",    agents: 3, runs: 0,    spend: "0.000", tags: ["sales"],                   nodes: [], edges: [] },
  { id: "wf-onchain", name: "On-chain Compliance Watch",  status: "active", updated: "5h ago",    agents: 2, runs: 642,  spend: "2.118", tags: ["compliance", "production"], nodes: [], edges: [] },
  { id: "wf-content", name: "Content Pipeline",           status: "draft",  updated: "1w ago",    agents: 5, runs: 0,    spend: "0.000", tags: ["marketing"],               nodes: [], edges: [] },
];

export const LOG_LINES = [
  { t: "09:00:00.012", lvl: "info", src: "trigger",        msg: "Chat trigger fired · run r-1842" },
  { t: "09:00:00.118", lvl: "info", src: "agent/support",  msg: "Routing classification → billing-question" },
  { t: "09:00:00.402", lvl: "pay",  src: "agent → alpaca", msg: "x402 settle 0.001 ALGO · ack 0x7a2f…" },
  { t: "09:00:01.118", lvl: "info", src: "agent/support",  msg: "Gemini 1.5 Pro · 412 tokens in, 218 out" },
  { t: "09:00:01.402", lvl: "a2a",  src: "agent → email",  msg: "Handoff payload · 1.4 KB · anchored 0x9b1c…" },
  { t: "09:00:02.221", lvl: "info", src: "action/email",   msg: "Sent to user@acme.com · 202 OK" },
  { t: "09:00:02.310", lvl: "ok",   src: "runtime",        msg: "Run r-1842 complete · 0.001 ALGO spent" },
];

export const WAITLIST_COUNT = 142;
