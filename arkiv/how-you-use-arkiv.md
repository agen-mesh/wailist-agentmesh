# How you use Arkiv

The product looks up a share by the entity key in the link. It does not filter on a column. `payload` is `bytes`, which the schema API does not let us index, and the graph is one blob rather than a set of columns we would query.

What fits the product:

- **Who can edit.** `readonly: true` on create. The author and the recipient both cannot change the shared graph. A share is a copy of the workflow at one moment, which is what the share page tells the recipient.
- **When it expires.** Seven days (`ExpirationTime.fromDays(7)`). The share screen says the link dies seven days after it is confirmed, and that AgentMesh does not keep a copy. When Rabdi returns `NoEntityFoundError`, the page shows that the link has died.
- **What the user sees from each Arkiv call.** `createEntity` in [write-share.mjs](https://github.com/agen-mesh/wailist-agentmesh/blob/feat/whitechain-x402/backend/arkiv-writer/write-share.mjs) is what produces the link and the Rabdi transaction the author can open. `getEntity` in [arkivShare.ts](https://github.com/agen-mesh/wailist-agentmesh/blob/feat/whitechain-x402/frontend/src/lib/arkivShare.ts) is what draws the workflow on [frontend/src/app/s/[key]/page.tsx](https://github.com/agen-mesh/wailist-agentmesh/blob/feat/whitechain-x402/frontend/src/app/s/%5Bkey%5D/page.tsx). The page checks `$creator` against `NEXT_PUBLIC_ARKIV_SHARE_ADDRESS` and ignores an entity written by anyone else.

A private share stores ciphertext. The AES-GCM key stays in the URL after `#k=` and is not sent to AgentMesh or written into the entity. A public share stores the gzipped graph, so `arkiv_getEntity` on a public key returns bytes the recipient can open without that fragment.

The query a judge can run is in [schema.md](schema.md). It filters on `arkiv.schema`, which is Arkiv's own tag, not a column we defined.
