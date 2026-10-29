# Integration feedback

Recorded during pre-event implementation starting 2026-10-30 Asia/Kolkata (2026-10-29 UTC).

- Current documentation and SDK 0.8.1 use Tiramisu `7738577`; older tutorials still show Kaolin. A versioned network/API compatibility table would make stale examples easier to identify. This integration checked the current network documentation, published package and an independent public RPC response.
- Native `expiresAt` is a hexadecimal block height, while this application also needs a UTC offer deadline. Explicit examples showing both clocks help prevent treating block height as Unix time. The reader tests the two boundaries separately.
- Typed attributes require different JSON wire encodings: `u64` is a hexadecimal quantity and `addr` is an address. A numeric string with type `str` cannot replace `u64`. Independent RPC fixtures cover these distinctions.
- SDK `getEntity` currently uses a key predicate query. The Go client follows that pattern and pins an `atBlock` context. Clear parity examples for direct JSON-RPC clients would reduce dependency on inspecting SDK source.
- SDK 0.8.1 package metadata returned no license field, and the upstream root `LICENSE` URL returned 404 during inspection. This is an unresolved dependency provenance question, not a claim that no license exists elsewhere. The central dependency owner has been notified.
- Public reads succeeded but the exact service predicate returned no offers. No live wallet write was attempted, so wallet compatibility, write fees, funding and write reliability remain unmeasured. Feedback about those paths would require an authorized live write.

Sources: [current network](https://docs.arkiv.network/networks/tiramisu/), [typed JSON-RPC](https://docs.arkiv.network/json-rpc/querying-data/), [SDK source](https://github.com/Arkiv-Network/arkiv-sdk-js), [published package](https://www.npmjs.com/package/@arkiv-network/sdk).
