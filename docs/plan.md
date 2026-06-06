# AgentMesh — Next.js Frontend Implementation Plan

## What We're Building

A Next.js frontend for AgentMesh — a no-code platform for building autonomous agent workflows with Algorand wallets and x402 micropayments.

**Design source:** Design handoff bundle (AgentMesh.html)
**Backend:** FastAPI (to be connected later — stubs marked with `// TODO: API`)

---

## Screens / Routes

| Route | Screen | Auth |
|-------|--------|------|
| `/` | Landing page | Public |
| `/signin` | Sign in | Public |
| `/signup` | Sign up | Public |
| `/workflows` | Workflows index | Protected |
| `/workflows/[id]` | Canvas editor | Protected |

---

## Design System

**Palette:** Violet-black dark theme
- `--bg: #08070C` / `--bg-elev-1: #0F0E18` / `--bg-elev-2: #161526` / `--bg-elev-3: #1F1D34`
- `--accent: #A78BFA` (violet)
- `--accent-strong: #8B5CF6`
- Magenta `#E879F9` for x402 nodes
- Warm `#FFB547` for wallet/balance

**Fonts:** Geist Sans, Geist Mono (both from Google Fonts / next/font)

**Animations:** fade-up, glow-pulse, drift, marquee-x, packet travel along edges

---

## Project Structure

```
agentmesh-new/
├── plan.md                          ← this file
├── next.config.ts
├── package.json
├── tailwind.config.ts               ← extended with design tokens
├── tsconfig.json
├── src/
│   ├── app/
│   │   ├── layout.tsx               ← root layout + CSS vars + fonts
│   │   ├── globals.css              ← design tokens (CSS vars), animations, utilities
│   │   ├── page.tsx                 → /  (Landing)
│   │   ├── signin/page.tsx          → /signin
│   │   ├── signup/page.tsx          → /signup
│   │   ├── workflows/
│   │   │   ├── page.tsx             → /workflows (index)
│   │   │   └── [id]/
│   │   │       └── page.tsx         → /workflows/[id] (canvas)
│   ├── components/
│   │   ├── ui/                      ← atomic: Logo, Pill, Button, Input, Tag, etc.
│   │   ├── landing/
│   │   │   ├── HeroSection.tsx
│   │   │   ├── LandingPillars.tsx
│   │   │   ├── LandingFlow.tsx
│   │   │   ├── LandingWaitlist.tsx
│   │   │   ├── LandingFooter.tsx
│   │   │   └── LogoMarquee.tsx
│   │   ├── auth/
│   │   │   ├── AuthForm.tsx
│   │   │   └── AuthVisual.tsx
│   │   ├── workflows/
│   │   │   ├── WorkflowsTopbar.tsx
│   │   │   ├── KpiCard.tsx
│   │   │   ├── WorkflowRow.tsx
│   │   │   └── WorkflowGrid.tsx
│   │   └── canvas/
│   │       ├── CanvasPage.tsx        ← top-level canvas shell + state
│   │       ├── CanvasTopbar.tsx
│   │       ├── CanvasGraph.tsx       ← SVG pan/zoom canvas + drag/drop
│   │       ├── nodes/
│   │       │   ├── TriggerNode.tsx
│   │       │   ├── AgentNode.tsx
│   │       │   ├── ProviderNode.tsx
│   │       │   ├── ToolNode.tsx
│   │       │   ├── Tool402Node.tsx
│   │       │   ├── ActionNode.tsx
│   │       │   └── EndNode.tsx
│   │       ├── EdgePath.tsx
│   │       ├── PalettePanel.tsx
│   │       ├── Inspector.tsx
│   │       └── LogDrawer.tsx
│   ├── lib/
│   │   ├── data.ts                  ← node templates, sample workflow, mock data
│   │   ├── types.ts                 ← Node, Edge, Workflow, PortPos types
│   │   ├── portUtils.ts             ← portWorld(), portForFrom(), isValidConnection()
│   │   └── api.ts                   ← FastAPI client stubs (TODO)
│   └── hooks/
│       ├── useAuth.ts               ← localStorage-based auth state (swap for real JWT later)
│       └── useWorkflow.ts           ← workflow CRUD state (swap for API later)
```

---

## Implementation Order

### Phase 1 — Scaffold (Next.js + styles)
1. `npx create-next-app` with TypeScript, App Router, Tailwind
2. Install: `geist` font package
3. Write `globals.css` — all CSS vars, keyframes, utility classes from design
4. Create shared UI atoms: Logo, Pill, Button, Input, Tag, Icons, Hairline, StatusDot

### Phase 2 — Auth pages
5. `/signin` and `/signup` — split layout (form left, visual right)
6. `useAuth` hook — localStorage `agentmesh_signed_in` flag, swap for real JWT later
7. Middleware for protected routes

### Phase 3 — Landing page
8. HeroSection with video bg, headline, "Open Studio" CTA
9. LogoMarquee (infinite scroll)
10. LandingPillars (4-card grid with hover)
11. LandingFlow (4-step row)
12. LandingWaitlist (email form)
13. LandingFooter

### Phase 4 — Workflows index
14. WorkflowsTopbar
15. KPI cards row
16. Search + status filter + row/grid toggle
17. WorkflowRow table + WorkflowGrid cards
18. Mock data in `lib/data.ts`

### Phase 5 — Canvas editor (hardest)
19. CanvasGraph — pan/zoom SVG, dot-grid background
20. All 7 node types (Trigger, Agent, Provider, Tool, Tool402, Action, End)
21. EdgePath — bezier edges, animated packets on run
22. Drag from palette onto canvas (HTML5 drag & drop)
23. Port-to-port wiring (mousedown → mousemove → mouseup)
24. PalettePanel — tabs, search, draggable rows
25. Inspector — per-node config forms
26. LogDrawer — collapsible console
27. CanvasTopbar — Deploy (assigns wallets), Run/Stop, stats
28. Toast notifications

---

## FastAPI Integration Points (stubs, wire up later)

All API calls are centralized in `src/lib/api.ts`. Each function has a `// TODO: API` comment and returns mock data by default.

```typescript
// src/lib/api.ts
export const api = {
  auth: {
    signIn: (email, pw) => ...,   // POST /auth/signin
    signUp: (email, pw, org) => ..., // POST /auth/signup
    signOut: () => ...,           // POST /auth/signout
  },
  workflows: {
    list: () => ...,              // GET /workflows
    get: (id) => ...,             // GET /workflows/:id
    create: (name) => ...,        // POST /workflows
    update: (id, wf) => ...,      // PUT /workflows/:id
    deploy: (id) => ...,          // POST /workflows/:id/deploy
    run: (id) => ...,             // POST /workflows/:id/run
    stop: (id) => ...,            // POST /workflows/:id/stop
  },
  agents: {
    fund: (wfId, agentId, amount) => ..., // POST /workflows/:id/agents/:agentId/fund
  },
  waitlist: {
    join: (email) => ...,         // POST /waitlist
  },
}
```

---

## Key Technical Decisions

- **No external canvas lib** — custom SVG pan/zoom (matches the design exactly, no ReactFlow overhead)
- **CSS vars for theming** — set on `<html>` element, no runtime overhead
- **localStorage auth stub** — `useAuth` hook returns `{ signedIn, signIn, signOut }`, trivially swappable
- **Next.js App Router** — server components where possible, client components only where interactivity needed
- **Drag & drop** — HTML5 native `draggable` + `onDragStart/onDrop` (same as prototype)
- **Fonts** — `next/font/google` with Geist Sans + Geist Mono, injected as CSS vars
