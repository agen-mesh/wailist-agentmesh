# Why Arkiv

The workflow the author is still editing stays in Postgres. The share does not.

If the shared graph stayed in our database, the recipient could open it only while AgentMesh is up and still stores that row. The share page tells them AgentMesh does not keep a copy, and that the link dies seven days after confirmation. A Postgres row would make that false. They would also lose a link that still opens when our API is down, because the browser reads Rabdi directly.

What we kept off Arkiv, and why:

- **Secrets, API keys, files, wallet keys, and connector config.** [sharePayload.ts](https://github.com/agen-mesh/wailist-agentmesh/blob/feat/whitechain-x402/frontend/src/lib/sharePayload.ts) strips them before the write. A share is a graph someone else will open. Those values are credentials.
- **The private-share key.** It lives in the URL fragment (`#k=`). The fragment is not sent to our API, and it is not a field on the entity. Putting it on Arkiv would make a "private" share readable by anyone who can query Rabdi.
- **The rate-limit counter.** `arkiv_share_meter` in Postgres counts 10 shares per user per hour and 200 per day. That counter exists to cap our writer's spend. It is not the thing the recipient came to see.

Prompts are included in the payload on purpose. They are part of the workflow the recipient is supposed to read.
