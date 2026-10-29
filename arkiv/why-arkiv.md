# Why Arkiv is used

Arkiv provides an independently queryable registry of owner-controlled, expiring service offers. Typed attributes let a client find a capability, chain and price without downloading every public payload. Native expiry removes obsolete listings from ordinary queries. An independent Go reader can discover and validate offers without trusting a browser selection or an application-local listing cache.

The integration needs owner identity, version and exact public terms to be checked again before approval and payment authorization. It binds those terms in a server-created snapshot. A changed listing requires renewed approval. Arkiv ownership controls a listing; it does not prove merchant reliability, guarantee fulfillment or revoke an already released payment authorization.

Private purchase coordination remains in the application's durable journal. Customer inputs, identity checks, payment signatures and results are excluded from public entities. Arc settlement and Arkiv publishing remain separate transactions, with no cross-chain atomicity claim.

Without the public registry, this implementation loses independent offer discovery and native listing expiry. The service-purchase implementation still needs authenticated approval, exact settlement evidence and merchant idempotency from the central integration. No live owner-signed write or completed purchase was demonstrated in this leaf.

Implementation dates are 2026-10-30 Asia/Kolkata (2026-10-29 UTC), before the event. This rationale is an engineering explanation and makes no award-eligibility claim.
