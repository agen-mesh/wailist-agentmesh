# AgentMesh x402 Weather Example

> **Source repo:** [github.com/YOUR_ORG/agentmesh-x402-weather](https://github.com/YOUR_ORG/agentmesh-x402-weather)
>
> This folder links to the standalone example repository. Clone that repo to run or fork it.

---

## Before you contribute

Read the [AgentMesh Whitepaper](WHITEPAPER_GOOGLE_DRIVE_LINK) to understand how x402 payments and the agent workflow model fit together. It's worth 10 minutes before diving in.

---

## What this is

A minimal Node.js server that implements the x402 payment protocol for weather data. An AgentMesh agent can:

1. Hit the endpoint with no payment → receive a `402` with price and parameter schema
2. Sign an Algorand micropayment (0.065 ALGO) from its own wallet
3. Retry with `X-Payment-Txid` → get real weather data for any city worldwide

Free APIs used: [Open-Meteo](https://open-meteo.com/) for weather, [Open-Meteo Geocoding](https://open-meteo.com/en/docs/geocoding-api) for city lookup. No API keys needed on your side either.

---

## How to run locally

**Requirements:** Node.js 18+, an Algorand testnet wallet address

```bash
git clone https://github.com/YOUR_ORG/agentmesh-x402-weather
cd agentmesh-x402-weather
npm install
RECIPIENT_ADDRESS=YOUR_ALGORAND_TESTNET_ADDRESS node server.js
```

Server starts on `http://localhost:4402`.

**Expose it publicly** (needed for AgentMesh agents to reach it):

```bash
cloudflared tunnel --url http://localhost:4402
```

Copy the `*.trycloudflare.com` URL and paste it into an x402 Tool node in AgentMesh.

---

## How it works

```
GET /weather?city=Tokyo
  → 402  X-Payment-Required: { price, recipient, params }
         body: { error: "Payment required", payment: {...} }

GET /weather?city=Tokyo  +  X-Payment-Txid: <algorand-txid>
  → verify txid on Algorand testnet (indexer + algod fallback)
  → 200  { city, temperature, humidity, wind_speed, conditions, ... }
```

The server uses [algosdk](https://github.com/algorand/js-algorand-sdk) to verify payments on-chain. It checks:
- Transaction exists and is a payment type
- `receiver` matches `RECIPIENT_ADDRESS`
- `amount` ≥ required microALGO (65000 μALGO = 0.065 ALGO)

Tries the Algorand Indexer first (confirmed transactions), falls back to algod pending pool for very recent transactions.

---

## Supported parameters

| Param     | Required | Description |
|-----------|----------|-------------|
| `city`    | yes      | City name (e.g. London, Tokyo, Mumbai) |
| `country` | no       | Country name to disambiguate |
| `state`   | no       | State/province to narrow the city |
| `units`   | no       | `celsius` (default) or `fahrenheit` |

AgentMesh's LLM reads these descriptions and chooses values autonomously.

---

## Contributing

To add your own x402 endpoint to the AgentMesh ecosystem, see the main [`CONTRIBUTING.md`](../CONTRIBUTING.md) for the full protocol spec and compatibility requirements.
