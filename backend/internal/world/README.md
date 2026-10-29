# World verification module

`New(ctx, Config, Store, *http.Client, ...Option)` constructs a confidential OIDC client. `WithClock` supports controlled tests. Production uses the trusted system clock and normal HTTPS transport. Configuration is default off and accepts only the exact World production or sandbox issuer paired with its environment.

The central adapter calls:

```go
Begin(context.Context, Binding) (Authorization, error)
Callback(context.Context, Binding, url.Values) (Evidence, error)
Cancel(context.Context, string, Binding) error
```

`HashState` supports a private, read-only lookup of the saved callback binding. The subsequent `Store.Consume` remains the atomic replay barrier. Never populate `Binding` from callback account/order parameters. `Binding.OrderID` and `Binding.IntentHash` must identify the immutable server-owned order. Existing identity links require `ExpectedSubject`; first enrollment requires `AllowEnrollment` and central uniqueness enforcement.

Implement the durable [Store contract](types.go). The callback consumes before exchange and returns evidence only after `Complete` succeeds. Failure never reopens an attempt. Cancellation must serialize with completion. There is no production memory store, account chooser, application-token minting, wallet linking, approval hook, signer permission or dispatcher here.

Only `Authorization` is a public response DTO. Keep `Evidence` in the backend and recheck its `ValidUntil` before a separately approved action. Do not log either authorization URLs or callback queries. See [setup, handler integration, failure paths and evidence limits](../../../docs/world-agent-pass.md).
