# How AgentMesh uses Arkiv

The integration publishes expiring service offers through a provider's connected browser wallet and discovers them through an independent Go JSON-RPC reader. The purchaser selects public terms; the central purchase coordinator obtains a merchant challenge and revalidates the stored snapshot before approval and authorization. Arkiv writes and Arc payments are separate transactions.

Public information is limited to typed service attributes, owner/payee addresses, HTTPS endpoint and structural input schema. Customer inputs, accounts, identity subjects, credentials, payment authorizations, workflow logs and fulfillment results stay off Arkiv. No public receipt writer is implemented in this leaf. A later receipt-publication failure must never repeat payment or fulfillment.

## Integration points

The backend exposes `NewClient(rpcURL, EndpointPolicy)`, `Query`, `Get` and `Revalidate` from `internal/arkiv`. An absent RPC URL or endpoint policy returns `ErrUnconfigured`. The package does not import the purchase coordinator. The central adapter converts these DTOs, authenticates requests, owns the shared endpoint policy and exposes the user-facing read routes.

The frontend exports `inspectProviderWallet`, `connectProviderWallet`, `publishOffer` and their types from `@/lib/arkiv`. `inspectProviderWallet` visibly distinguishes unconfigured, disconnected, wrong-network, ready and error states for the UI. `publishOffer` accepts `{provider, rpcURL, expectedAccount, offer, validateEndpoint}`. Wire `validateEndpoint` to the central authenticated `purchaseAPI.preflightOfferEndpoint(endpoint)` wrapper. No credentials or private key are accepted by this helper.

The helper awaits wallet connection/network switching, validates public terms, awaits endpoint preflight, then checks actual account, chain and both expiry conditions again at the send boundary. The SDK result is awaited through receipt confirmation. Success returns `{entityKey, txHash, expiresAtBlock, owner}`. Errors never return fixture entities. A send timeout or uncertain receipt remains `write-uncertain`, retaining a transaction hash when available and blocking an automatic second send. Inspect wallet activity before retrying.

The intended visible paths are the provider offer form and discovery list under `/purchases`, owned by the UI and central integration workers. This leaf supplies the helper and reader; its isolated checks do not establish a running page, authenticated route, screenshot or completed purchase. Final application tests and media belong to the combined integration.

## Reproduce a read

These fish commands make public read-only calls. They request no accounts, funds or signatures. They require curl and Python 3. Failure to receive the expected chain or block must stop the investigation; it is not a reason to invent an offer.

```fish
set rpc https://rpc.tiramisu.db-chain.testnet.arkiv.network
set chain (curl --fail-with-body --silent --show-error --max-time 15 --json '{"jsonrpc":"2.0","id":1,"method":"eth_chainId","params":[]}' $rpc | python3 -c 'import json,sys; print(json.load(sys.stdin)["result"])')
test "$chain" = 0x7614d1; or exit 1
set block (curl --fail-with-body --silent --show-error --max-time 15 --json '{"jsonrpc":"2.0","id":2,"method":"eth_blockNumber","params":[]}' $rpc | python3 -c 'import json,sys; print(json.load(sys.stdin)["result"])')
set predicate "project = str('agentmesh-mumbai') AND kind = str('service-offer-v1') AND settlement_chain = u64(5042002) AND status = str('active')"
python3 -c 'import json,sys; print(json.dumps({"jsonrpc":"2.0","id":3,"method":"arkiv_query","params":[sys.argv[1],{"atBlock":sys.argv[2],"limit":"0x14","select":{"key":True,"owner":True,"updatedAt":True,"expiresAt":True,"contentType":True,"payload":True,"attributes":True}}]}))' "$predicate" "$block" | curl --fail-with-body --silent --show-error --max-time 15 -H 'Content-Type: application/json' --data-binary @- $rpc
```

On 2026-10-29 at 20:09:11 UTC (2026-10-30, Asia/Kolkata), an independent read-only check received HTTP 200 for this exact predicate at block `0x12b9dc`, with `data: []` and that block number. That check selected key/expiry only. Its raw response SHA-256 was `189581d4e8f9c70fb445577e797fdc7ceb1d0928fabc103b745e7fad48e0b14d`. A separate public query found an unrelated entity. Neither response proves a matching offer or write access.

## Evidence still required

No owner-signed offer was published in this work. No provider address, usable signer, funded write, faucet request or Arc settlement is claimed. A separately authorized task must configure a provider wallet privately, publish a real offer, independently query its complete projection, and exercise expiry and changed-offer behavior in the running application. Record its actual transaction, entity, owner and dates. Fixtures and simulations do not satisfy those live gates.

Implementation began on 2026-10-30 Asia/Kolkata (2026-10-29 UTC), before the event. No event or Studio provenance is inferred. Documentation and local checks alone do not establish sponsor qualification.
