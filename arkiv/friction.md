# Friction

## The blob we have to store cannot be queried

`payload` has to be `bytes` because the share is one gzip or one ciphertext. The schema API cannot index `bytes`. We expected to list "shares this wallet wrote" by owner. Selecting `owner` on `arkiv.schema = str('agentmesh.share.v1')` shows `0x49A83B71f4EbbA4d7D645F4AdAE013BC835Cfbf7` and a count of 8. Adding `AND owner = addr(0x49A83B71f4EbbA4d7D645F4AdAE013BC835Cfbf7)` to that same query returned `"count": "0x0"` at block `0x4d5a3`.

What would help today: one worked example of "entities of schema S owned by address A" when the only application field is `bytes`, and a filter that matches the `owner` value `select` already returns.

Reproduce:

```bash
curl https://rpc.rabdi.db-chain.testnet.arkiv.network/ \
  -H 'content-type: application/json' \
  --data-binary @- <<'EOF'
{"jsonrpc":"2.0","id":1,"method":"arkiv_query","params":["arkiv.schema = str('agentmesh.share.v1')",{"select":{"count":true}}]}
EOF

curl https://rpc.rabdi.db-chain.testnet.arkiv.network/ \
  -H 'content-type: application/json' \
  --data-binary @- <<'EOF'
{"jsonrpc":"2.0","id":1,"method":"arkiv_query","params":["arkiv.schema = str('agentmesh.share.v1') AND owner = addr(0x49A83B71f4EbbA4d7D645F4AdAE013BC835Cfbf7)",{"select":{"count":true}}]}
EOF
```

The first returned `"count": "0x8"`. The second returned `"count": "0x0"`.

## A timed-out write still spends the daily cap

`AllowShare` commits the meter row before `createEntity`. The write budget is 30 seconds. We expected a failed write to leave the cap unchanged, or to return the entity key if Rabdi had already accepted it. What happens: the user gets `share_unconfirmed`, the attempt still counts toward 200 shares a day, and the entity may exist with no key in the response. The code says this on purpose in [shares.go](https://github.com/agen-mesh/wailist-agentmesh/blob/feat/whitechain-x402/backend/internal/api/handlers/shares.go), because a timeout must not be a way around the cap. The missing piece is a way to ask Rabdi for the entity a given transaction created after the HTTP call has already given up.

## An empty access key looks like Arkiv is down

The public read uses `rabdi({ accessKey })`. With `NEXT_PUBLIC_ARKIV_ACCESS_KEY` empty, `getEntity` throws and the page says "Couldn't reach Arkiv" even when the entity is on the public RPC. We expected the public RPC to answer a single `getEntity` without a key, and a missing key to be a distinct error from a dead link. The public curl queries in this folder do succeed with no key. The browser client does not.
