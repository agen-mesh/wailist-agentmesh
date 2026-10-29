# Service offer schema v1

Implemented starting 2026-10-30 Asia/Kolkata (2026-10-29 UTC). This is pre-event work. These files establish neither event eligibility nor successful live publishing.

The current target is Arkiv Tiramisu, chain `7738577` (`0x7614d1`), RPC `https://rpc.tiramisu.db-chain.testnet.arkiv.network`. SDK `@arkiv-network/sdk` version `0.8.1` and `viem` version `2.57.13` were checked against published packages on that date. [Official network](https://docs.arkiv.network/networks/tiramisu/), [RPC format](https://docs.arkiv.network/json-rpc/querying-data/).

An offer has exactly these nine indexed attributes:

| Name | Arkiv type | Required value |
| --- | --- | --- |
| `project` | `str` | `agentmesh-mumbai` |
| `kind` | `str` | `service-offer-v1` |
| `capability` | `str` | Lowercase identifier matching `[a-z][a-z0-9_.-]{0,63}` |
| `settlement_chain` | `u64` | `5042002`, Arc testnet |
| `asset` | `addr` | `0x3600000000000000000000000000000000000000` |
| `price_micros` | `u64` | Positive integer USDC units, six decimal places |
| `version` | `u64` | Positive publisher version |
| `status` | `str` | `active` for selectable offers |
| `valid_until` | `u64` | UTC Unix seconds, at most `253402300799` |

RPC `u64` values are canonical hexadecimal quantities such as `0xf4240`, not JSON numbers or decimal strings. Browser drafts and server snapshot JSON represent these integers as decimal strings to avoid rounding. `settlement_chain` is a fixed, safely representable JSON number in the snapshot API.

The content type is exactly `application/json`. Payload bytes are UTF-8 JSON, at most 16 KiB. Only these fields are accepted:

```json
{
  "endpoint": "https://merchant.example/tool",
  "method": "POST",
  "payee": "0x2222222222222222222222222222222222222222",
  "input_schema": {
    "type": "object",
    "properties": { "prompt": { "type": "string" } },
    "required": ["prompt"],
    "additionalProperties": false
  }
}
```

This is a fixture, not a deployed merchant or an observed writer address. `payee` must equal the entity's current owner. A transfer of listing ownership cannot silently keep the old payee. Ownership establishes control of the listing, not service quality or fulfillment.

Input schemas support at most 32 named scalar fields: `string`, `integer`, `number`, or `boolean`. Names match `[A-Za-z][A-Za-z0-9_]{0,63}` and exclude `constructor` and `prototype`. Required names must exist and cannot repeat. Nested objects, references, descriptions, defaults, examples and unknown fields are unsupported. The schema describes input shape; actual input values remain private.

Endpoint syntax requires HTTPS without user information, query parameters or fragments. The Go reader requires an injected endpoint policy. The browser writer requires an awaited backend preflight callback. The central integration owns the common private-address, DNS, redirect and dial rules. Syntax checking is insufficient by itself, and this module exposes no private-address bypass. A later merchant request must enforce the same policy at connection time.

## Expiry and snapshot binding

`expiresAt` is the native block height from the entity metadata. Reject an offer when `expiresAt <= currentBlock`. Independently reject it when `valid_until <= current UTC Unix seconds`. Do not convert a block height into a timestamp. The browser uses `ExpirationTime.atBlock` and verifies the returned absolute expiry. [SDK expiration](https://docs.arkiv.network/typescript-sdk/mutating-data/).

`ContentHash` is `0x` followed by SHA-256 of the original decoded payload bytes. `SnapshotHash` is SHA-256 of `agentmesh-arkiv-snapshot-v1\n` followed by Go JSON encoding of the normalized `OfferSnapshot`, with `snapshot_hash` empty and `observed_at_block` zero. It binds entity key, current owner, version, content hash, capability, chain, token, price, status, UTC deadline, native expiry, update block and parsed payload. Source field order is part of this versioned encoding; maps use Go's deterministic JSON key order. Neither hash establishes merchant trust or a payment receipt.

Store the complete server-read snapshot with an order. `Revalidate` checks its commitment and obtains a fresh entity snapshot before comparison. Changed price, payload or owner fails even without a version increment. Native extension and update-block changes also require a new approval. The observation block can advance without changing the commitment. The order owner must invoke this before approval and authorization; a browser-supplied snapshot is never authoritative. Listing expiry or changes do not revoke an already released payment authorization.

## Read contract

`Client.Query` uses the fixed predicate below, with optional validated capability and integer price filters. It returns valid offers, a rejection count, the query block, expression and optional cursor. Malformed, expired and endpoint-policy-rejected records are excluded; malformed RPC envelopes fail the request. `Client.Get` uses a `$key = key(...)` predicate, as SDK 0.8.1 does, and returns a strict snapshot or a typed error. Both check the RPC chain and pin the query to an observed block. Pagination must retain its returned block and cursor. Native expiry still compares against the current head.

```text
project = str('agentmesh-mumbai') AND kind = str('service-offer-v1') AND settlement_chain = u64(5042002) AND status = str('active')
```

The RPC endpoint is trusted infrastructure configured by the server operator. These reads do not verify inclusion proofs. Implementation: [Go snapshot validator](../backend/internal/arkiv/offers.go), [Go RPC client](../backend/internal/arkiv/client.go), [browser writer](../frontend/src/lib/arkiv/wallet.ts).
