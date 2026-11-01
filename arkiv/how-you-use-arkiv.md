# How AgentMesh uses Arkiv

The integration targets Rabdi `7794095`, publishing expiring service offers through a provider's connected browser wallet and discovering them through an independent Go JSON-RPC reader. The purchaser selects public terms; the central purchase coordinator obtains a merchant challenge and revalidates the stored snapshot before approval and authorization. Arkiv writes and Arc payments are separate transactions.

Public information is limited to typed service attributes, owner/payee addresses, HTTPS endpoint and structural input schema. Customer inputs, accounts, identity subjects, credentials, payment authorizations, workflow logs and fulfillment results stay off Arkiv. No public receipt writer or publication outbox is included.

## Integration points

The backend exposes `NewClient(rpcURL, EndpointPolicy)`, `Query`, `Get` and `Revalidate` from `internal/arkiv`. An absent RPC URL or endpoint policy returns `ErrUnconfigured`. The package does not import the purchase coordinator. The central adapter converts these DTOs, authenticates requests, owns the shared endpoint policy and exposes the user-facing read routes.

The frontend exports `inspectProviderWallet`, `connectProviderWallet`, `publishOffer` and their types from `@/lib/arkiv`. `inspectProviderWallet` visibly distinguishes unconfigured, disconnected, wrong-network, ready and error states for the UI. `publishOffer` accepts `{provider, rpcURL, expectedAccount, offer, validateEndpoint}`. Wire `validateEndpoint` to the central authenticated `purchaseAPI.preflightOfferEndpoint(endpoint)` wrapper. No credentials or private key are accepted by this helper.

The helper requires SDK `1.0.0` and `viem` `2.57.13`. It uses `rabdi({accessKey: ""})` with an explicitly configured public browser RPC URL, never a private backend key. Connection and network switching are awaited, then actual account, wallet chain, read RPC chain and both expiry conditions are checked again at the send boundary. The SDK result is awaited through receipt confirmation. A separate SDK public client reads the entity and checks its key, owner, all twelve attributes, exact payload bytes, version and native expiry against a fresh block. Readback also rechecks the account, expected chain and UTC deadline.

Success returns `{entityKey, txHash, expiresAtBlock, owner}` only after an observed send, matching receipt result and exact readback. Errors never return fixture entities. A send timeout, uncertain receipt or mismatched readback remains `write-uncertain`, retaining the observed transaction hash when available and blocking an automatic second send within that attempt. Inspect wallet activity before initiating another attempt.

The new server snapshot includes `arkiv_chain_id` and a v2 digest. Missing or different source networks reject before catalog RPC. Historical stored orders and hashes are not rewritten. An unsigned old offer reference must be recreated from a current Rabdi offer. Reconciliation of an already released Arc authorization belongs to the central coordinator and retains the original intent independently of catalog migration.

The intended visible paths are the provider offer form and discovery list under `/purchases`, owned by the UI and central integration workers. This leaf supplies the helper and reader; its isolated checks do not establish a running page, authenticated route, screenshot or completed purchase. Final application tests and media belong to the combined integration.

## Reproduce a read

These fish commands make public read-only calls to Rabdi and print a raw current-state page. They request no accounts, funds or signatures and were not executed during this migration. They require curl and Python 3. Failure to receive the expected chain or block must stop the investigation. The application reader additionally validates every offer and the endpoint policy; this raw command is not an approval authority. Public RPC access is rate limited. [Official read methods](https://docs.arkiv.network/json-rpc/querying-data/).

```fish
set rpc https://rpc.rabdi.db-chain.testnet.arkiv.network
set chain (curl --fail-with-body --silent --show-error --max-time 15 --json '{"jsonrpc":"2.0","id":1,"method":"eth_chainId","params":[]}' $rpc | python3 -c 'import json,sys; print(json.load(sys.stdin)["result"])')
test "$chain" = 0x76edaf; or exit 1
set before (curl --fail-with-body --silent --show-error --max-time 15 --json '{"jsonrpc":"2.0","id":2,"method":"eth_blockNumber","params":[]}' $rpc | python3 -c 'import json,sys; print(json.load(sys.stdin)["result"])')
set predicate "arkiv.schema = str('agentmesh.service-offer.v2') AND project = str('agentmesh-mumbai') AND kind = str('service-offer-v2') AND settlement_chain = u64(5042002) AND status = str('active') ORDER BY \$key ASC"
set response (python3 -c 'import json,sys; print(json.dumps({"jsonrpc":"2.0","id":3,"method":"arkiv_query","params":[sys.argv[1],{"limit":"0x14","offset":"0x0","select":{"key":True,"owner":True,"updatedAt":True,"expiresAt":True,"attributes":True}}]}))' "$predicate" | curl --fail-with-body --silent --show-error --max-time 15 -H 'Content-Type: application/json' --data-binary @- $rpc | string collect)
set chain (curl --fail-with-body --silent --show-error --max-time 15 --json '{"jsonrpc":"2.0","id":4,"method":"eth_chainId","params":[]}' $rpc | python3 -c 'import json,sys; print(json.load(sys.stdin)["result"])')
test "$chain" = 0x76edaf; or exit 1
set after (curl --fail-with-body --silent --show-error --max-time 15 --json '{"jsonrpc":"2.0","id":5,"method":"eth_blockNumber","params":[]}' $rpc | python3 -c 'import json,sys; print(json.load(sys.stdin)["result"])')
printf '%s\n' "$response" | python3 -c 'import json,sys; d=json.load(sys.stdin); assert d["jsonrpc"]=="2.0" and d["id"]==3 and "error" not in d; r=d["result"]; assert isinstance(r["data"],list) and int(sys.argv[1],16)<=int(r["blockNumber"],16)<=int(sys.argv[2],16); print(json.dumps(d))' "$before" "$after"
```

Historical evidence: on 2026-10-29 at 20:09:11 UTC (2026-10-30, Asia/Kolkata), an independent Tiramisu read-only check received HTTP 200 for the old v1 project/kind/settlement/status predicate at pinned block `0x12b9dc`, with `data: []`. It selected key/expiry only. Its raw response SHA-256 was `189581d4e8f9c70fb445577e797fdc7ceb1d0928fabc103b745e7fad48e0b14d`. A separate public query found an unrelated entity. These are not results of the new Rabdi command above and prove no Rabdi offer availability or write access.

## Evidence still required

The migration's focused Go test/build/vet and 46 offline wallet controls plus strict helper typecheck passed. The helper checks used SDK `1.0.0`, `viem` `2.57.13`, Vitest `5.0.0`, TypeScript `6.0.2` and a controlled provider. Of those controls, 45 mock SDK actions; one runs the actual released SDK encoder and send-request path, captures its calldata and stops before broadcast. Readback is mocked in these checks. They establish local behavior, not live wallet, receipt or RPC access. Full application integration and browser evidence remain separate checks on the combined source.

No owner-signed offer was published by this migration leaf. No provider address, usable signer, funded write, faucet request or Arc settlement is claimed. A separately authorized task must configure a provider wallet privately, publish a real Rabdi offer, independently query its complete projection, and exercise expiry and changed-offer behavior in the running application. Record its actual transaction, entity, owner and dates. Fixtures and simulations do not satisfy those live gates.

Implementation began on 2026-10-30 Asia/Kolkata (2026-10-29 UTC); Rabdi migration preparation occurred on 2026-10-31. These are pre-event dates. No event or Studio provenance is inferred. Documentation and local checks alone do not establish sponsor qualification.
