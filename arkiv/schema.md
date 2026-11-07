# Schema

One entity type, `agentmesh.share.v1`, defined in [backend/arkiv-writer/shareSchema.mjs](https://github.com/agen-mesh/wailist-agentmesh/blob/feat/whitechain-x402/backend/arkiv-writer/shareSchema.mjs).

| Field | Type | Indexed | What it holds |
|---|---|---|---|
| `payload` | `bytes` | no (`bytes` cannot be indexed) | The shared workflow. A public share is the gzipped graph. A private share is that graph sealed with AES-GCM. |

The app does not filter on `payload`. The recipient already has the entity key in `/s/<key>`. The read is `arkiv_getEntity` of that key.

Owner and creator are the writer wallet `0x49A83B71f4EbbA4d7D645F4AdAE013BC835Cfbf7`. A query of the schema on Rabdi (chain `7794095`) returned 8 entities, and the rows that were selected named this address as both `owner` and `creator`.

Each write sets `readonly: true` and `expirationDays: 7` in [backend/arkiv-writer/write-share.mjs](https://github.com/agen-mesh/wailist-agentmesh/blob/feat/whitechain-x402/backend/arkiv-writer/write-share.mjs). The recipient cannot edit the entity. It expires seven days after the write.

List them:

```bash
curl https://rpc.rabdi.db-chain.testnet.arkiv.network/ \
  -H 'content-type: application/json' \
  --data-binary @- <<'EOF'
{"jsonrpc":"2.0","id":1,"method":"arkiv_query","params":["arkiv.schema = str('agentmesh.share.v1')",{"select":{"key":true,"creator":true,"owner":true,"expiresAt":true},"limit":"0x5"}]}
EOF
```

That query returned `"count": "0x8"` at block `0x4d5a3` on 2026-11-07. One key from that page is `0x733019e94394f8d46c312e5bcd014bbe11d5e3b94b1ea58b91a00779676cff93`.
