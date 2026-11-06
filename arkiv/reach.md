# Existing product and reach

AgentMesh already has a workflow execution backend and a browser workflow editor. This work extends that existing product with public service-offer discovery and a provider-wallet publishing helper. Relevant repository entry points are the [workflow runner](../backend/internal/engine/runner.go), [canvas inspector](../frontend/src/components/canvas/Inspector.tsx), [Arc screen](../frontend/src/components/arc/ArcPage.tsx) and [shared API client](../frontend/src/lib/api.ts).

The intended users are service providers publishing expiring offers and customers selecting a service for a separately approved purchase. The combined integration owns the discovery page and purchase journey; this isolated module does not demonstrate that journey yet.

No measured active-user count, organization count, adoption rate, revenue, writer count or external distribution figure was provided or established in this work. No such metric is claimed. The historical Tiramisu v1 public query returned zero matching offers at its recorded observation block; it establishes no Rabdi adoption or availability.

The original leaf began on 2026-10-30 Asia/Kolkata (2026-10-29 UTC); Rabdi migration preparation occurred on 2026-10-31. Public repository licensing, any event-specific new work and successful live usage must be established separately. Existing product code and these pre-event additions must be disclosed as prior work.
