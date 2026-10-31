# Service offer schema v2

Implemented starting 2026-10-30 Asia/Kolkata (2026-10-29 UTC). This is pre-event work. These files establish neither event eligibility nor successful live publishing.

Migration preparation on 2026-10-31 Asia/Kolkata targets Rabdi, chain `7794095` (`0x76edaf`), RPC `https://rpc.rabdi.db-chain.testnet.arkiv.network`. Current official documentation identifies Rabdi as the public testnet and says Tiramisu was replaced. Entities do not carry over. This is public-document confirmation, not a sponsor response, funding proof or event qualification. [Current network](https://docs.arkiv.network/networks/rabdi/), [older networks](https://docs.arkiv.network/networks/older-networks/).

The original 2026-10-30 implementation used Tiramisu `7738577`, SDK `0.8.1` and `viem` `2.57.13`. Those historical checks establish no Rabdi behavior. The published SDK `1.0.0` tarball was inspected on 2026-10-31: it exports `rabdi({accessKey})`, `defineSchema` and the schema-based create API. It accepts the existing `viem` `2.57.13` peer. Public browser configuration must never contain a private backend access key. [Published SDK](https://www.npmjs.com/package/@arkiv-network/sdk/v/1.0.0).

Rabdi offers have exactly twelve typed attributes. The named SDK schema `agentmesh.service-offer.v2` adds `arkiv.schema` automatically. Index the schema tag and the fields used in filters: `project`, `kind`, `capability`, `settlement_chain`, `price_micros` and `status`. The remaining fields are stored without an index; `bytes` cannot be indexed. [Schema definitions](https://docs.arkiv.network/schemas/define/).

| Name | Arkiv type | Required value |
| --- | --- | --- |
| `arkiv.schema` | `str` | `agentmesh.service-offer.v2` |
| `project` | `str` | `agentmesh-mumbai` |
| `kind` | `str` | `service-offer-v2` |
| `capability` | `str` | Lowercase identifier matching `[a-z][a-z0-9_.-]{0,63}` |
| `settlement_chain` | `u64` | `5042002`, Arc testnet |
| `asset` | `addr` | `0x3600000000000000000000000000000000000000` |
| `price_micros` | `u64` | Positive integer USDC units, six decimal places |
| `version` | `u64` | Positive publisher version |
| `status` | `str` | `active` for selectable offers |
| `valid_until` | `u64` | UTC Unix seconds, at most `253402300799` |
| `content_type` | `str` | `application/json` |
| `payload` | `bytes` | Hex-encoded UTF-8 JSON described below |

RPC `u64` values are canonical hexadecimal quantities such as `0xf4240`, not JSON numbers or decimal strings. Browser drafts and server snapshot JSON represent these integers as decimal strings to avoid rounding. `settlement_chain` is a fixed, safely representable JSON number in the snapshot API.

Payload bytes are UTF-8 JSON, at most 16 KiB. Rabdi carries them in the `payload` bytes attribute, with `content_type` alongside it. The old top-level `payload` and `contentType` fields are not accepted as a substitute. Logical payload fields and the content hash remain unchanged. Only these fields are accepted:

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

`expiresAt` is the native block height from the entity metadata. Reject an offer when `expiresAt <= currentBlock`. Independently reject it when `valid_until <= current UTC Unix seconds`. Do not convert a block height into a timestamp. SDK `1.0.0` writes with `createEntity(schema, values, {expiration: ExpirationTime.atBlock(height)})`, keeping the absolute deadline distinct from a duration. [SDK expiration](https://docs.arkiv.network/writing/expiration/).

`ContentHash` is `0x` followed by SHA-256 of the original decoded payload bytes. `SnapshotHash` is SHA-256 of `agentmesh-arkiv-snapshot-v2\n` followed by Go JSON encoding of the normalized `OfferSnapshot`, with `snapshot_hash` empty and `observed_at_block` zero. The new `ArkivChainID uint64`, JSON `arkiv_chain_id`, binds the source network independently of the Arc settlement chain. The digest also binds entity key, current owner, version, content hash, capability, token, price, status, UTC deadline, native expiry, update block and parsed payload. Source field order is part of this versioned encoding; maps use Go's deterministic JSON key order. Neither hash establishes merchant trust or a payment receipt.

`Revalidate` rejects a missing or different `arkiv_chain_id` with `ErrSnapshotNetwork` before any catalog RPC. An identical entity key on Rabdi cannot make an old snapshot portable. Adding a chain field to old approved content cannot upgrade a v1 digest. Never rewrite stored source, intent, quote or hash bytes. Unsigned legacy references must be recreated from a current offer. Already released Arc authorizations must reconcile under their original intent without fresh Rabdi discovery; the central purchase coordinator owns that compatibility path.

Store the complete server-read snapshot with an order. `Revalidate` checks its commitment and obtains a fresh entity snapshot before comparison. Changed price, payload or owner fails even without a version increment. Native extension and update-block changes also require a new approval. The observation block can advance without changing the commitment. The order owner must invoke this before approval and authorization; a browser-supplied snapshot is never authoritative. Listing expiry or changes do not revoke an already released payment authorization.

## Read contract

Constructor signatures remain `NewClient(rpcURL, policy)` and `NewClientWithHTTP(rpcURL, policy, client)`. `QueryOptions` contains `Capability`, `MaxPriceMicros`, `Limit` and `Offset`; the maximum page size is 50. `OfferPage` contains `Offers`, `Rejected`, `BlockNumber`, `Offset` and `Query`. There is no cursor or historical block option.

`Client.Query` uses the fixed predicate below, optional validated capability and integer price filters, and `ORDER BY $key ASC`. `Client.Get` uses `$key = key(0x...)` with limit 1. Both send `arkiv_query` with exactly `select`, hexadecimal `limit` and hexadecimal `offset`. The projection selects `key`, `owner`, `updatedAt`, `expiresAt` and `attributes`. The expected result is `{"data":[...],"blockNumber":"0x..."}` with each attribute encoded as `{"name":"...","type":"...","value":"..."}`. No `atBlock`, cursor or top-level payload projection is sent. [Read methods](https://docs.arkiv.network/json-rpc/querying-data/).

The reader checks chain and head before and after the latest-state query. The response block must lie between those heads, an entity update cannot be newer than the response block, and native expiry is checked against the later head. Malformed, expired and endpoint-policy-rejected records are excluded from lists; malformed RPC responses fail the request. Every approval revalidation queries again. Separate offset pages may repeat or omit records when the chain changes, so a list is not a historical snapshot or approval authority.

```text
arkiv.schema = str('agentmesh.service-offer.v2') AND project = str('agentmesh-mumbai') AND kind = str('service-offer-v2') AND settlement_chain = u64(5042002) AND status = str('active')
```

The RPC endpoint is trusted infrastructure configured by the server operator. These reads do not verify inclusion proofs. Implementation: [Go snapshot validator](../backend/internal/arkiv/offers.go), [Go RPC client](../backend/internal/arkiv/client.go), [browser writer](../frontend/src/lib/arkiv/wallet.ts).
