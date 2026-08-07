<div align="center">

# AgentMesh

### Give your AI agents a wallet. Let them pay their own way.

AgentMesh is a no-code platform for building autonomous AI agent workflows — where agents don't just think, they act. Each agent gets its own Algorand wallet and pays for the APIs it uses in real time, on-chain, without you touching a single API key.

**[→ Try AgentMesh](https://www.agent-mesh.app)**

</div>

---

## What is this?

Most AI agent platforms make you sign up for every API your agent needs, manage credentials, and pay flat subscriptions whether the agent uses them or not.

AgentMesh flips that. Instead of API keys, agents use **money** — tiny Algorand micropayments, settled on-chain in under 5 seconds. Your agent sees a paid API, pays for it from its own wallet, and gets the data back. No accounts. No keys. No subscriptions.

You build the workflow visually. AgentMesh handles everything else.

---

## How it works

**Build** — Open the canvas. Drag out an Agent, connect some tools, add an action. Wire them together. No code required.

**Configure** — Choose your LLM (Gemini, OpenAI, Anthropic, Groq, or Mistral). Write a system prompt. For any paid API tool, just paste the URL and click **Discover** — AgentMesh reads the price and parameters automatically.

**Deploy** — Hit deploy. AgentMesh creates a real Algorand wallet for your agent. Add a small amount of ALGO to fund it.

**Run** — Your agent works autonomously. It decides which tools to call, pays for each one from its wallet, and delivers the result. Watch every step stream live in the logs.

---

## The x402 payment flow

When an agent hits a paid API:

1. The API responds with a price and an Algorand wallet address
2. The agent signs a payment transaction from its own wallet
3. The agent retries the request with the transaction ID
4. The API verifies the payment on-chain and returns the data

The whole cycle takes under 5 seconds. Algorand transaction fees are ~$0.0002, which makes per-call pricing viable even at fractions of a cent.

---

## Why Algorand

| | |
|---|---|
| **Fees** | ~$0.0002 per transaction — sub-cent API pricing actually works |
| **Speed** | ~3.5s finality — fast enough for synchronous pay-and-retry |
| **Simplicity** | No smart contracts needed — a basic payment transfer is all it takes |

---

## What's live today

| Feature | Status |
|---|---|
| Visual drag-and-drop canvas | ✅ |
| 5 LLM providers, 20+ models (Gemini, OpenAI, Anthropic, Groq, Mistral) | ✅ |
| Agentic tool-calling loop (up to 15 iterations per run) | ✅ |
| x402 paid API tool nodes with one-click discovery | ✅ |
| Algorand wallet per agent — deploy, fund, check balance | ✅ |
| Live streaming run logs | ✅ |
| Email action node | ✅ |
| GitHub + Google OAuth, email/password auth | ✅ |
| Tendril compute — rent a machine by the hour, SSH from the console | ✅ |

---

## What's coming

- **Standard HTTP tools** — call any API, not just x402 ones
- **Webhook + schedule triggers** — run agents on a timer or HTTP event
- **Memory nodes** — agents that remember across runs
- **Run history** — browse and replay past runs
- **Agent-to-agent calls** — deploy a workflow as an x402 endpoint, let other agents call and pay it
- **x402 marketplace** — browse published paid APIs, add to canvas in one click
- **On-chain run receipts** — immutable audit trail anchored on Algorand

---

## Build your own x402 API

Want to publish a paid API that AgentMesh agents can discover and pay for? See the [`x402/`](./x402/) folder — it has the full protocol spec and a working example to fork.

---

## Contributing

See [`CONTRIBUTING.md`](./CONTRIBUTING.md) to get started.
