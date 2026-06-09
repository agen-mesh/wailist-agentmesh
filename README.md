# AgentMesh

**Build AI agents that pay for what they use — no API keys, no subscriptions, no middleman.**

AgentMesh is a no-code visual platform where you drag, wire, and run autonomous AI agent workflows. Each agent gets its own Algorand wallet and pays for APIs on the spot using real micropayments. The agent decides when to call a tool, signs an Algorand transaction from its own wallet, and gets the data — all without you managing a single API key.

[**Try it →**](https://www.agent-mesh.app)

---

## The problem with AI agents today

To give an agent access to an API, you need to:
- Create an account with the provider
- Pay for a subscription tier you may not fully use
- Manage and rotate API keys
- Hope the rate limit matches your agent's actual usage

Subscriptions are designed for humans. Micropayments are designed for machines.

---

## How AgentMesh works

**1. Build on the canvas**
Drag out nodes — a Trigger, an Agent, x402 Tool nodes, an Action. Wire them together. No code.

**2. Configure your agent**
Pick an LLM provider (Gemini, OpenAI, Anthropic, Groq, or Mistral). Write a system prompt. Add paid API tools with one click — hit "Discover" and AgentMesh auto-reads the price and parameters straight from the endpoint.

**3. Deploy**
AgentMesh generates a real Algorand wallet for your agent. Fund it with a small amount of ALGO.

**4. Run**
Your agent runs autonomously. It calls tools when it needs them, pays per call from its own wallet, and synthesises results. Watch every step live in the streaming log.

---

## The x402 payment protocol

x402 is how agents pay for APIs without human involvement:

```
Agent hits an API  →  gets a 402 with price + recipient address
Agent signs an Algorand transaction from its wallet
Agent retries with the transaction ID as proof
API server verifies on-chain  →  returns the data
```

The full round-trip takes under 5 seconds. 0.001 ALGO transaction fees (~$0.0002) make even sub-cent API calls viable.

---

## Why Algorand

- **Fees**: ~0.001 ALGO per transaction (~$0.0002). Sub-cent API pricing is actually profitable.
- **Finality**: ~3.5 seconds. Fast enough for synchronous pay-and-retry in a single agent run.
- **Simplicity**: No smart contracts needed. A basic ALGO transfer is all it takes.

---

## What's working today

- Visual workflow canvas — drag, drop, wire, configure
- 5 LLM providers: Gemini, OpenAI, Anthropic, Groq, Mistral (20+ models)
- Full agentic tool-calling loop — agent decides which tools to call and how many times (up to 15 iterations)
- x402 paid API tool nodes with one-click parameter discovery
- Algorand wallet per agent — deploy, fund, check balance in the UI
- Email action node (send results via email)
- Live streaming logs — watch every node execute in real time
- GitHub + Google OAuth and email/password auth

---

## Building x402-compatible endpoints

Want to publish a paid API that AgentMesh agents can use? See the [`x402-examples/`](./x402-examples/) folder:

- **[`CONTRIBUTING.md`](./x402-examples/CONTRIBUTING.md)** — the full protocol spec and how to build a compatible endpoint
- **[`weather-example/`](./x402-examples/weather-example/)** — a working reference implementation

---

## Roadmap

- Standard HTTP tool node (non-x402 APIs)
- Anthropic function-calling loop
- Webhook + schedule triggers
- Run history and log browser
- Memory nodes (conversation memory, vector store / RAG)
- Agent-to-agent calls — a deployed workflow callable as an x402 tool by another agent
- x402 endpoint marketplace — browse and one-click add paid APIs
- On-chain run receipts on Algorand

---

## Contributing / local dev

See [`backend/`](./backend/) and [`frontend/`](./frontend/) for setup instructions. The backend is Go + PostgreSQL; the frontend is Next.js 16 with a custom SVG canvas.
