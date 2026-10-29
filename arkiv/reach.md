# Existing product and reach

AgentMesh already has a workflow execution backend and a browser workflow editor. This work extends that existing product with public service-offer discovery and a provider-wallet publishing helper. Relevant repository entry points are the [workflow runner](../backend/internal/engine/runner.go), [canvas inspector](../frontend/src/components/canvas/Inspector.tsx), [Arc screen](../frontend/src/components/arc/ArcPage.tsx) and [shared API client](../frontend/src/lib/api.ts).

The intended users are service providers publishing expiring offers and customers selecting a service for a separately approved purchase. The combined integration owns the discovery page and purchase journey; this isolated module does not demonstrate that journey yet.

No measured active-user count, organization count, adoption rate, revenue, writer count or external distribution figure was provided or established in this work. No such metric is claimed. The independent exact public query returned zero matching offers at the recorded observation block.

All code and documentation in this leaf began on 2026-10-30 Asia/Kolkata (2026-10-29 UTC), before the event. Public repository licensing, any event-specific new work and successful live usage must be established separately. Existing product code and these pre-event additions must be disclosed as prior work.
