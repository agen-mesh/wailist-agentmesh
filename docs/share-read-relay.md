# Share preview relay

Server-rendered share pages and Open Graph images call the Go backend. Without visitor identity, Railway sees the Next server's egress address, so unrelated visitors share the 60-read-per-minute bucket.

## Deployment prerequisite

Set a new, dedicated SHARE_READ_RELAY_KEY to the same 64 hexadecimal characters on the Vercel frontend and Railway backend. Generate it privately with openssl rand -hex 32. Keep it in server environment settings, never a NEXT_PUBLIC_ variable, repository file, screenshot, log or PR description. Do not reuse a session-signing or encryption key.

Set the frontend's server-only BACKEND_URL to the backend HTTPS origin, without a path, credentials, query or fragment. Restart the backend and redeploy the frontend after configuring the key. A malformed nonempty backend key fails startup without printing its value. An unset key disables signed relay requests. Ordinary public reads remain available, but server-rendered share details and personalized Open Graph images require the configured relay.

This change supplies source and documentation only. It does not provision either deployment setting.

## Trust boundary

Vercel supplies VERCEL=1 and replaces incoming address metadata. Only its x-vercel-forwarded-for value is accepted by the frontend helper. Generic x-forwarded-for, x-real-ip, cookies and authorization headers are never copied. Do not set VERCEL=1 on a public self-hosted Next server and assume caller-provided headers become trusted.

Railway's public edge can replace forwarded addresses. The relay therefore carries authenticated visitor metadata in dedicated headers instead of relying on X-Forwarded-For surviving a second proxy. Scope is limited to GET /shares/{token}. Payment limits, authentication and global proxy handling are unchanged.

The X-AgentMesh-Share-Signature is a lowercase hexadecimal HMAC-SHA256 over these newline-separated fields: v1, GET, exact escaped /shares/token path, canonical visitor IP, and integer Unix timestamp in seconds.

X-AgentMesh-Share-Visitor and X-AgentMesh-Share-Timestamp carry the last two fields. IPv4-mapped IPv6 is normalized to IPv4. Other IPv6 uses its compressed lowercase form. No cookies, relay key or signature are returned in share content or metadata. Redirects are rejected to prevent forwarding the signature to another upstream.

Any relay header requires all three headers, each with one nonempty value, and a configured key. The backend validates the address, method, path, absence of a query, and signature using constant-time comparison. Timestamps may be at most 60 seconds old or 5 seconds in the future. Invalid, partial or stale relay requests return 401 before a database read; they never fall back to the connecting proxy's bucket. Replays within the short window still consume the same visitor's rate limit. Keep deployment clocks synchronized.

Requests without relay headers retain the existing direct-client limiter. That limiter is an in-process, per-replica deterrent, with the repository's existing direct proxy assumptions. A relay signature grants no account access and bypasses no read limit.

## Local verification and native exports

A private loopback backend may use HTTP for isolated checks. A local test ingress must explicitly replace visitor metadata before forwarding to the Next process; a caller-supplied header on an exposed self-hosted server is not proof of identity. Tests cover both the frontend trust decision and real backend quota separation.

The static native export returns before reading request headers or relay configuration. Native and ordinary browser API requests continue to use the direct public share endpoint.

Rotate the dedicated key on both services together. During a mismatch, relay reads fail closed; direct public reads remain available. Do not log the dedicated relay headers.

## References

- [Vercel request headers](https://vercel.com/docs/headers/request-headers)
- [Next request headers](https://nextjs.org/docs/app/api-reference/functions/headers)
- [Railway networking specifications](https://docs.railway.com/networking/public-networking/specs-and-limits)
- [Railway staff explanation of forwarded-address replacement](https://station.railway.com/questions/security-critical-questions-on-edge-prox-8fddd775)
