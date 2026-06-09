# Building x402-Compatible Endpoints for AgentMesh

AgentMesh agents pay for API calls using the x402 protocol — an implementation of HTTP's long-dormant `402 Payment Required` status code as a real micropayment standard on Algorand.

This guide explains exactly how to build an endpoint that AgentMesh's x402 tool node can discover, pay, and call autonomously.

---

## Before you start

Read the [AgentMesh Whitepaper](WHITEPAPER_GOOGLE_DRIVE_LINK) to understand the full context of how agents, payments, and tools fit together.

---

## How the x402 flow works

When an AgentMesh agent calls your endpoint:

1. **First request — no payment**
   Agent sends `GET /your-endpoint?param=value` with no payment header.

2. **Your server responds `402 Payment Required`**
   You return status `402` with:
   - `X-Payment-Required` header containing a JSON payment descriptor
   - The same JSON in the response body (Cloudflare and some proxies strip non-standard headers)

3. **Agent reads the payment descriptor**
   It extracts the price, recipient address, and network from your response.

4. **Agent signs and submits an Algorand transaction**
   From its own wallet, to your `recipient` address, for exactly the `price` in ALGO.

5. **Agent retries with payment proof**
   `GET /your-endpoint?param=value` with `X-Payment-Txid: <algorand-txid>` header.

6. **You verify on-chain and return data**
   Look up the transaction on Algorand testnet/mainnet, confirm recipient + amount, then return your actual response.

---

## The payment descriptor format

Your `402` response must include this JSON — both as the `X-Payment-Required` header value and in the response body:

```json
{
  "price":       "0.065",
  "unit":        "call",
  "network":     "algorand-testnet",
  "recipient":   "YOUR_ALGORAND_ADDRESS",
  "description": "What your API does in one sentence.",
  "params": [
    {
      "name":        "city",
      "type":        "string",
      "required":    true,
      "description": "City name to get weather for (e.g. London, Tokyo)"
    },
    {
      "name":        "units",
      "type":        "string",
      "required":    false,
      "description": "celsius or fahrenheit",
      "default":     "celsius"
    }
  ]
}
```

| Field         | Type     | Description |
|---------------|----------|-------------|
| `price`       | string   | Amount in ALGO (e.g. `"0.065"`) |
| `unit`        | string   | Always `"call"` for per-call pricing |
| `network`     | string   | `"algorand-testnet"` or `"algorand-mainnet"` |
| `recipient`   | string   | Your Algorand wallet address that receives payment |
| `description` | string   | Shown to the user in the AgentMesh UI |
| `params`      | array    | Parameter schema — becomes LLM function declarations |

### The `params` array

This is the most important part. AgentMesh's "Discover" button fetches your endpoint, reads the `params` schema, and registers them as **function call parameters** for the LLM. The AI agent uses these descriptions to decide what values to pass when calling your tool.

- Write `description` values that make sense to an LLM. Be specific: "City name (e.g. London, Tokyo, Mumbai)" is better than "city".
- `required: true` params must always be supplied by the LLM.
- `default` values are shown as hints in the UI.
- Supported types: `string`, `number`, `boolean`.

---

## Response body format (send your payment info in both places)

Always send the payment descriptor in **both** the header and body. Cloudflare tunnels and some proxies strip `X-Payment-Required`:

```javascript
const payment = { price: "0.065", unit: "call", network: "algorand-testnet", recipient: RECIPIENT, params: PARAMS };

res.setHeader("X-Payment-Required", JSON.stringify(payment));
res.setHeader("Content-Type", "application/json");
res.writeHead(402);
res.end(JSON.stringify({ error: "Payment required", payment }));
```

---

## Verifying the payment

When you receive a request with `X-Payment-Txid`, verify on Algorand before returning data.

**Using the Algorand indexer (Node.js):**

```javascript
const algosdk = require("algosdk");
const indexer = new algosdk.Indexer("", "https://testnet-idx.algonode.cloud", "");

async function verifyPayment(txId, recipientAddress, requiredMicroAlgo) {
  const res = await indexer.lookupTransactionByID(txId).do();
  const tx  = res.transaction;

  if (!tx["payment-transaction"]) throw new Error("not a payment transaction");

  const receiver = tx["payment-transaction"].receiver;
  const amount   = tx["payment-transaction"].amount;

  if (receiver !== recipientAddress) throw new Error(`wrong recipient: ${receiver}`);
  if (amount < requiredMicroAlgo)    throw new Error(`amount too low: ${amount}`);

  return true;
}
```

> `requiredMicroAlgo = Math.round(priceAlgo * 1_000_000)` — e.g. 0.065 ALGO = 65000 μALGO.

**Fallback for in-flight transactions** (not yet indexed — use `algod.pendingTransactionInformation`):

```javascript
const algod = new algosdk.Algodv2("", "https://testnet-api.algonode.cloud", "");

async function verifyPending(txId, recipientAddress, requiredMicroAlgo) {
  const ptx  = await algod.pendingTransactionInformation(txId).do();
  const raw  = ptx.txn?.txn;
  if (!raw) throw new Error("tx not found");
  const receiver = raw.rcv ? algosdk.encodeAddress(Buffer.from(raw.rcv, "base64")) : "";
  const amount   = raw.amt ?? 0;
  if (receiver !== recipientAddress) throw new Error("wrong recipient");
  if (amount < requiredMicroAlgo)    throw new Error("insufficient amount");
  return true;
}
```

Always try the indexer first (confirmed) and fall back to algod pending (in-flight).

---

## Exposing localhost with Cloudflare Tunnel

AgentMesh agents run in production and can't reach `localhost`. Use [Cloudflare Tunnel](https://developers.cloudflare.com/cloudflare-one/connections/connect-networks/do-more-with-tunnels/trycloudflare/) to expose your local server:

```bash
cloudflared tunnel --url http://localhost:4402
```

This gives you a public `*.trycloudflare.com` URL you can paste into the AgentMesh x402 Tool node.

> **Note:** Because Cloudflare may strip the `X-Payment-Required` header, always send payment info in the response body too (see above).

---

## Adding your endpoint to AgentMesh

1. In the canvas, drag out an **x402 Tool** node.
2. Paste your public endpoint URL.
3. Click **Discover** — AgentMesh fetches your endpoint, reads the `402` response, and auto-fills the price and parameter schema.
4. Wire the tool to an Agent node via the **attach** port.
5. Deploy the workflow and fund the agent's Algorand wallet.
6. Run — the agent LLM decides when and with what parameters to call your tool.

---

## See a working example

The [`weather-example/`](./weather-example/) folder points to a fully working x402 weather endpoint built exactly to this spec. Use it as a reference or fork it as a starting point.
