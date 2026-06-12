import { NodeTypeMeta, Workflow, WorkflowNode, WorkflowEdge, MarketplaceEndpoint, MarketplaceWorkflow } from "./types";

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

export const MARKETPLACE_ENDPOINTS: MarketplaceEndpoint[] = [
  { id: "mp-tavily",     name: "Tavily Search",      description: "Real-time web search optimised for AI agents. Returns structured results with snippets, URLs, and relevance scores.", provider: "tavily.x402",     price: "0.002",  unit: "call",  category: "search",  icon: "⌕", tags: ["search","web","research"],          author: "Tavily Inc.",      calls: 128400, rating: 4.8, featured: true },
  { id: "mp-firecrawl",  name: "Firecrawl Scrape",   description: "Turn any URL into clean markdown for LLM ingestion. Handles SPAs, paywalls, and JS-heavy pages.",                  provider: "firecrawl.x402",  price: "0.005",  unit: "page",  category: "data",    icon: "◐", tags: ["scraping","markdown","web"],         author: "Firecrawl",        calls: 84200,  rating: 4.6, featured: true },
  { id: "mp-flux",       name: "FluxImage Gen",       description: "State-of-the-art image generation via Flux.1. High-resolution output, fast inference, prompt adherence.",           provider: "flux.x402",       price: "0.020",  unit: "image", category: "ai",      icon: "✦", tags: ["image","generation","creative"],     author: "Black Forest Labs", calls: 31700,  rating: 4.9, featured: true },
  { id: "mp-alpaca",     name: "AlpacaQuote",         description: "Live and historical stock/crypto quotes from Alpaca Markets. Streaming and snapshot modes.",                         provider: "alpaca.x402",     price: "0.001",  unit: "quote", category: "finance", icon: "$", tags: ["finance","stocks","crypto"],         author: "Alpaca Markets",   calls: 249000, rating: 4.7 },
  { id: "mp-ocr",        name: "OCR.space",           description: "Extract text from images and PDFs. Supports 30+ languages and table detection.",                                     provider: "ocr.x402",        price: "0.003",  unit: "page",  category: "ai",      icon: "⊟", tags: ["ocr","pdf","text-extraction"],       author: "OCR.space",        calls: 56800,  rating: 4.4 },
  { id: "mp-weather",    name: "WeatherKit",          description: "Real-time and forecast weather for any city worldwide. Temperature, wind, precipitation, UV index.",                  provider: "weatherkit.x402", price: "0.0008", unit: "call",  category: "data",    icon: "◌", tags: ["weather","forecast","geo"],          author: "WeatherKit",       calls: 189300, rating: 4.5 },
  { id: "mp-perplexity", name: "Perplexity Search",   description: "AI-powered answer engine with citations. Best for knowledge questions and fact-checking.",                           provider: "perplexity.x402", price: "0.008",  unit: "query", category: "search",  icon: "◎", tags: ["search","ai","answers"],             author: "Perplexity AI",    calls: 42100,  rating: 4.7 },
  { id: "mp-exa",        name: "Exa Neural Search",   description: "Semantic search over the live web. Finds conceptually similar content rather than keyword matches.",                 provider: "exa.x402",        price: "0.004",  unit: "call",  category: "search",  icon: "⟲", tags: ["search","semantic","neural"],        author: "Exa",              calls: 18900,  rating: 4.6 },
];

export const MARKETPLACE_WORKFLOWS: MarketplaceWorkflow[] = [
  {
    id: "mwf-support", name: "Customer Support Triage", author: "AgentMesh Team",
    description: "Classifies inbound support tickets, looks up account data, drafts a reply, and routes to the right team — fully automated.",
    tags: ["support","classification","email"], nodes: 6, runs: 12400, stars: 284, featured: true, price: "2.00",
    previewNodes: [{ type: "trigger", label: "Webhook", template: "webhook" }, { type: "agent", label: "Classifier" }, { type: "tool402", label: "CRM Lookup" }, { type: "agent", label: "Reply Drafter" }, { type: "action", label: "Send Email", template: "email" }, { type: "end", label: "Done", template: "end" }],
  },
  {
    id: "mwf-market", name: "Daily Market Brief", author: "AgentMesh Team",
    description: "Pulls live prices, searches recent news, synthesises a morning brief, and emails it to your team at 7 AM.",
    tags: ["finance","research","schedule"], nodes: 5, runs: 1820, stars: 147, featured: true, price: "1.50",
    previewNodes: [{ type: "trigger", label: "Schedule", template: "cron" }, { type: "tool402", label: "AlpacaQuote" }, { type: "tool402", label: "Tavily Search" }, { type: "agent", label: "Brief Writer" }, { type: "action", label: "Send Email", template: "email" }],
  },
  {
    id: "mwf-leads", name: "Lead Enrichment Pipeline", author: "sales-tools",
    description: "Takes a CSV of company names, enriches each with web scraping and LinkedIn data, and writes structured profiles to your CRM.",
    tags: ["sales","enrichment","crm"], nodes: 7, runs: 4300, stars: 203, price: "3.00",
    previewNodes: [{ type: "trigger", label: "Webhook", template: "webhook" }, { type: "tool402", label: "Firecrawl" }, { type: "tool402", label: "Exa Search" }, { type: "agent", label: "Enricher" }, { type: "action", label: "CRM Write", template: "webhook" }, { type: "end", label: "Done", template: "end" }],
  },
  {
    id: "mwf-content", name: "Content to Social Pipeline", author: "marketing-kit",
    description: "Feed in a blog post URL — the agent scrapes it, generates a Twitter thread, LinkedIn post, and image, then schedules them.",
    tags: ["marketing","social","content"], nodes: 8, runs: 2100, stars: 118, price: "2.50",
    previewNodes: [{ type: "trigger", label: "Webhook", template: "webhook" }, { type: "tool402", label: "Firecrawl" }, { type: "agent", label: "Thread Writer" }, { type: "tool402", label: "FluxImage" }, { type: "action", label: "Post to X", template: "webhook" }, { type: "action", label: "Post to LinkedIn", template: "webhook" }],
  },
];

export interface WorkflowTemplate {
  id: string;
  name: string;
  description: string;
  tags: string[];
  icon: string;
  previewNodes: Array<{ type: string; label: string }>;
  nodes: WorkflowNode[];
  edges: WorkflowEdge[];
}

export const WORKFLOW_TEMPLATES: WorkflowTemplate[] = [
  {
    id: "tpl-lead-research",
    name: "Lead Research Agent",
    description: "Drop in a company name — the agent searches the web and emails you a structured research brief.",
    tags: ["research", "sales", "email"],
    icon: "⌕",
    previewNodes: [
      { type: "trigger", label: "Manual" },
      { type: "agent",   label: "Researcher" },
      { type: "tool402", label: "Tavily Search" },
      { type: "action",  label: "Email report" },
      { type: "end",     label: "Done" },
    ],
    nodes: [
      { id: "n1", type: "trigger",  template: "manual",  x: 60,  y: 200, label: "Manual Trigger", icon: "▶" },
      { id: "n2", type: "agent",    template: "agent",   x: 340, y: 160,
        name: "Lead Researcher", icon: "◇",
        systemPrompt: "You are a lead research agent. The user will give you a company name or domain. Use the Tavily Search tool to find: what the company does, their key products, estimated company size, recent news, and any funding information. Return a structured report with sections: Overview, Products, Size & Stage, Recent News." },
      { id: "n3", type: "provider", template: "gemini",  x: 260, y: 400, name: "Gemini 2.5 Flash", model: "gemini-2.5-flash", icon: "G" },
      { id: "n4", type: "tool402",  template: "tavily",  x: 500, y: 400, name: "Tavily Search", icon: "⌕",
        description: "Real-time web search optimised for AI agents. Returns structured results with snippets and URLs.",
        price: "0.002", unit: "call" },
      { id: "n5", type: "action",   template: "email",   x: 700, y: 160, name: "Send Report Email", icon: "✉" },
      { id: "n6", type: "end",      template: "done",    x: 960, y: 170, label: "Done", icon: "■" },
    ] as WorkflowNode[],
    edges: [
      { id: "e1", from: "n1", to: "n2", kind: "flow",   toPort: "in" },
      { id: "e2", from: "n3", to: "n2", kind: "attach", toPort: "model" },
      { id: "e3", from: "n4", to: "n2", kind: "attach", toPort: "tools" },
      { id: "e4", from: "n2", to: "n5", kind: "flow",   toPort: "in" },
      { id: "e5", from: "n5", to: "n6", kind: "flow",   toPort: "in" },
    ] as WorkflowEdge[],
  },
  {
    id: "tpl-market-brief",
    name: "Daily Market Brief",
    description: "Run it each morning — pulls live stock quotes and web headlines, then emails your team a concise brief.",
    tags: ["finance", "research", "email"],
    icon: "$",
    previewNodes: [
      { type: "trigger", label: "Manual" },
      { type: "agent",   label: "Analyst" },
      { type: "tool402", label: "AlpacaQuote" },
      { type: "tool402", label: "Tavily" },
      { type: "action",  label: "Email brief" },
      { type: "end",     label: "Done" },
    ],
    nodes: [
      { id: "n1", type: "trigger",  template: "manual",  x: 60,  y: 200, label: "Manual Trigger", icon: "▶" },
      { id: "n2", type: "agent",    template: "agent",   x: 340, y: 150,
        name: "Market Analyst", icon: "◇",
        systemPrompt: "You are a financial analyst agent. Use AlpacaQuote to fetch the latest prices for SPY, QQQ, BTC/USD, and ETH/USD. Use Tavily Search to find the top 3 market-moving headlines from today. Then compose a concise morning brief: key levels, whether markets are risk-on or risk-off, and the most important headline. Keep it under 200 words, professional tone." },
      { id: "n3", type: "provider", template: "gemini",  x: 220, y: 390, name: "Gemini 2.5 Flash", model: "gemini-2.5-flash", icon: "G" },
      { id: "n4", type: "tool402",  template: "alpaca",  x: 440, y: 390, name: "AlpacaQuote", icon: "$",
        description: "Live stock and crypto quotes from Alpaca Markets.",
        price: "0.001", unit: "quote" },
      { id: "n5", type: "tool402",  template: "tavily",  x: 440, y: 510, name: "Tavily Search", icon: "⌕",
        description: "Real-time web search for market news and headlines.",
        price: "0.002", unit: "call" },
      { id: "n6", type: "action",   template: "email",   x: 720, y: 150, name: "Send Morning Brief", icon: "✉" },
      { id: "n7", type: "end",      template: "done",    x: 980, y: 160, label: "Done", icon: "■" },
    ] as WorkflowNode[],
    edges: [
      { id: "e1", from: "n1", to: "n2", kind: "flow",   toPort: "in" },
      { id: "e2", from: "n3", to: "n2", kind: "attach", toPort: "model" },
      { id: "e3", from: "n4", to: "n2", kind: "attach", toPort: "tools" },
      { id: "e4", from: "n5", to: "n2", kind: "attach", toPort: "tools" },
      { id: "e5", from: "n2", to: "n6", kind: "flow",   toPort: "in" },
      { id: "e6", from: "n6", to: "n7", kind: "flow",   toPort: "in" },
    ] as WorkflowEdge[],
  },
  {
    id: "tpl-content-pipeline",
    name: "Content Pipeline",
    description: "Two-agent system: first agent scrapes a URL and researches the topic, second agent writes a polished article and emails it.",
    tags: ["content", "marketing", "multi-agent"],
    icon: "◐",
    previewNodes: [
      { type: "trigger", label: "Manual" },
      { type: "agent",   label: "Researcher" },
      { type: "tool402", label: "Firecrawl" },
      { type: "agent",   label: "Writer" },
      { type: "action",  label: "Email draft" },
      { type: "end",     label: "Done" },
    ],
    nodes: [
      { id: "n1", type: "trigger",  template: "manual",    x: 60,  y: 200, label: "Manual Trigger", icon: "▶" },
      { id: "n2", type: "agent",    template: "agent",     x: 300, y: 160,
        name: "Researcher", icon: "◇",
        systemPrompt: "You are a research agent. The user will give you a URL or topic. Use Firecrawl to scrape the URL into clean text, then extract: main thesis, key facts and data points, notable quotes, and related angles worth exploring. Return a structured research brief that a writer can use." },
      { id: "n3", type: "provider", template: "gemini",    x: 220, y: 390, name: "Gemini 2.5 Flash", model: "gemini-2.5-flash", icon: "G" },
      { id: "n4", type: "tool402",  template: "firecrawl", x: 440, y: 390, name: "Firecrawl Scrape", icon: "◐",
        description: "Turn any URL into clean markdown for LLM ingestion.",
        price: "0.005", unit: "page" },
      { id: "n5", type: "agent",    template: "agent",     x: 660, y: 160,
        name: "Writer", icon: "◇",
        systemPrompt: "You are a content writer agent. You will receive a research brief from the Researcher agent. Write a polished, engaging article: compelling headline, strong intro hook, 3-4 body sections with subheadings, and a clear conclusion. Target 600-800 words. Tone: professional but readable. Do not fabricate facts not in the research brief." },
      { id: "n6", type: "provider", template: "gemini",    x: 580, y: 390, name: "Gemini 2.5 Flash", model: "gemini-2.5-flash", icon: "G" },
      { id: "n7", type: "action",   template: "email",     x: 1000, y: 160, name: "Email Draft", icon: "✉" },
      { id: "n8", type: "end",      template: "done",      x: 1260, y: 170, label: "Done", icon: "■" },
    ] as WorkflowNode[],
    edges: [
      { id: "e1", from: "n1", to: "n2", kind: "flow",   toPort: "in" },
      { id: "e2", from: "n3", to: "n2", kind: "attach", toPort: "model" },
      { id: "e3", from: "n4", to: "n2", kind: "attach", toPort: "tools" },
      { id: "e4", from: "n2", to: "n5", kind: "flow",   toPort: "in" },
      { id: "e5", from: "n6", to: "n5", kind: "attach", toPort: "model" },
      { id: "e6", from: "n5", to: "n7", kind: "flow",   toPort: "in" },
      { id: "e7", from: "n7", to: "n8", kind: "flow",   toPort: "in" },
    ] as WorkflowEdge[],
  },
];
