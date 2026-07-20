"use client";
import { useState, useRef, useEffect } from "react";
import { WorkflowNode, Workflow, PortName } from "@/lib/types";
import { NODE_TYPES } from "@/lib/data";
import {
  portWorld,
  portForFrom,
  portForTo,
  isValidConnection,
} from "@/lib/portUtils";
import { CanvasNode } from "./nodes";

interface CanvasGraphProps {
  workflow: Workflow;
  setWorkflow: React.Dispatch<React.SetStateAction<Workflow>>;
  selectedId: string | null;
  setSelectedId: (id: string | null) => void;
  deployed: boolean;
  running: boolean;
  attachedSummaries: Record<string, { model: string | null; tools: number }>;
}

interface ViewState {
  x: number;
  y: number;
  k: number;
}
interface WireState {
  fromId: string;
  fromPort: PortName;
  x: number;
  y: number;
}
interface HoverPort {
  nodeId: string;
  port: PortName;
}

export function CanvasGraph({
  workflow,
  setWorkflow,
  selectedId,
  setSelectedId,
  deployed,
  running,
  attachedSummaries,
}: CanvasGraphProps) {
  const wrapRef = useRef<HTMLDivElement>(null);
  const [view, setView] = useState<ViewState>({ x: 40, y: 40, k: 0.95 });
  const [panning, setPanning] = useState(false);
  const panRef = useRef({ active: false, sx: 0, sy: 0, ox: 0, oy: 0 });
  const dragRef = useRef<{
    id: string;
    sx: number;
    sy: number;
    ox: number;
    oy: number;
  } | null>(null);
  const wireRef = useRef<{ fromId: string; fromPort: PortName } | null>(null);
  const [wire, setWire] = useState<WireState | null>(null);
  const [hoverPort, setHoverPort] = useState<HoverPort | null>(null);
  // Value intentionally unread: the tick's only job is forcing re-renders
  // while a run is active so the animated edges advance.
  const [, setAnimTick] = useState(0);

  // Latest-value refs for the window mousemove/mouseup listeners below. Those
  // handlers need current view/hover/workflow, but depending on them directly
  // made the effect tear down and re-register both global listeners on every
  // frame of a pan or drag (view changes each mousemove). Reading through refs
  // lets the effect subscribe once on mount and still see fresh values.
  const viewRef = useRef(view);
  const hoverPortRef = useRef(hoverPort);
  const workflowRef = useRef(workflow);
  const setWorkflowRef = useRef(setWorkflow);
  useEffect(() => {
    viewRef.current = view;
    hoverPortRef.current = hoverPort;
    workflowRef.current = workflow;
    setWorkflowRef.current = setWorkflow;
  });

  useEffect(() => {
    if (!running) return;
    const id = setInterval(() => setAnimTick((t) => t + 1), 90);
    return () => clearInterval(id);
  }, [running]);

  // Wheel zoom
  useEffect(() => {
    const el = wrapRef.current;
    if (!el) return;
    const onWheel = (e: WheelEvent) => {
      e.preventDefault();
      const rect = el.getBoundingClientRect();
      const mx = e.clientX - rect.left,
        my = e.clientY - rect.top;
      setView((v) => {
        const dk = -e.deltaY * 0.0015;
        const k2 = Math.min(2, Math.max(0.3, v.k * (1 + dk)));
        const f = k2 / v.k;
        return { x: mx - (mx - v.x) * f, y: my - (my - v.y) * f, k: k2 };
      });
    };
    el.addEventListener("wheel", onWheel, { passive: false });
    return () => el.removeEventListener("wheel", onWheel);
  }, []);

  const onBgMouseDown = (e: React.MouseEvent) => {
    if (e.button !== 0 && e.button !== 1) return;
    const target = e.target as HTMLElement;
    if (target.closest("[data-node]") || target.closest("[data-port]")) return;
    panRef.current = {
      active: true,
      sx: e.clientX,
      sy: e.clientY,
      ox: view.x,
      oy: view.y,
    };
    setPanning(true);
    setSelectedId(null);
  };

  useEffect(() => {
    const onMove = (e: MouseEvent) => {
      if (panRef.current.active) {
        const dx = e.clientX - panRef.current.sx;
        const dy = e.clientY - panRef.current.sy;
        setView((v) => ({
          ...v,
          x: panRef.current.ox + dx,
          y: panRef.current.oy + dy,
        }));
      }
      if (dragRef.current) {
        const { id, sx, sy, ox, oy } = dragRef.current;
        const dx = (e.clientX - sx) / viewRef.current.k;
        const dy = (e.clientY - sy) / viewRef.current.k;
        setWorkflowRef.current((wf) => ({
          ...wf,
          nodes: wf.nodes.map((n) =>
            n.id === id ? { ...n, x: ox + dx, y: oy + dy } : n,
          ),
        }));
      }
      if (wireRef.current && wrapRef.current) {
        const rect = wrapRef.current.getBoundingClientRect();
        const v = viewRef.current;
        setWire((w) =>
          w
            ? {
                ...w,
                x: (e.clientX - rect.left - v.x) / v.k,
                y: (e.clientY - rect.top - v.y) / v.k,
              }
            : w,
        );
      }
    };

    const onUp = () => {
      panRef.current.active = false;
      setPanning(false);
      dragRef.current = null;

      const hp = hoverPortRef.current;
      if (wireRef.current && hp && hp.nodeId !== wireRef.current.fromId) {
        const nodes = workflowRef.current.nodes;
        const fromNode = nodes.find((n) => n.id === wireRef.current!.fromId);
        const toNode = nodes.find((n) => n.id === hp.nodeId);
        if (
          fromNode &&
          toNode &&
          isValidConnection(fromNode, wireRef.current.fromPort, toNode, hp.port)
        ) {
          const kind =
            hp.port === "model" || hp.port === "tools" ? "attach" : "flow";
          setWorkflowRef.current((wf) => ({
            ...wf,
            edges: [
              ...wf.edges,
              {
                id: `e_${Date.now()}`,
                from: fromNode.id,
                to: toNode.id,
                kind,
                toPort: hp.port,
              },
            ],
          }));
        }
      }
      wireRef.current = null;
      setWire(null);
    };

    window.addEventListener("mousemove", onMove);
    window.addEventListener("mouseup", onUp);
    return () => {
      window.removeEventListener("mousemove", onMove);
      window.removeEventListener("mouseup", onUp);
    };
    // Subscribes once: all mutable values are read through refs above.
  }, []);

  const removeEdge = (id: string) =>
    setWorkflow((wf) => ({
      ...wf,
      edges: wf.edges.filter((e) => e.id !== id),
    }));

  const onDrop = (e: React.DragEvent) => {
    e.preventDefault();
    const data = e.dataTransfer.getData("application/agentmesh");
    if (!data || !wrapRef.current) return;
    let meta: Partial<WorkflowNode>;
    try {
      meta = JSON.parse(data);
    } catch {
      return; // malformed payload — ignore the drop rather than throw
    }
    const rect = wrapRef.current.getBoundingClientRect();
    const t = NODE_TYPES[meta.type!];
    const x = (e.clientX - rect.left - view.x) / view.k - (t ? t.w / 2 : 90);
    const y = (e.clientY - rect.top - view.y) / view.k - (t ? t.h / 2 : 30);
    const id = `n_${Date.now()}`;
    const node: WorkflowNode = { id, x, y, ...meta } as WorkflowNode;
    setWorkflow((wf) => ({ ...wf, nodes: [...wf.nodes, node] }));
    setSelectedId(id);
  };

  const startNodeDrag = (e: React.MouseEvent, n: WorkflowNode) => {
    if ((e.target as HTMLElement).closest("[data-port]")) return;
    e.stopPropagation();
    setSelectedId(n.id);
    dragRef.current = {
      id: n.id,
      sx: e.clientX,
      sy: e.clientY,
      ox: n.x,
      oy: n.y,
    };
  };

  const startWire = (
    e: React.MouseEvent,
    nodeId: string,
    fromPort: PortName,
  ) => {
    e.stopPropagation();
    const n = workflow.nodes.find((x) => x.id === nodeId);
    if (!n) return;
    const p = portWorld(n, fromPort);
    wireRef.current = { fromId: nodeId, fromPort };
    setWire({ fromId: nodeId, fromPort, x: p.x, y: p.y });
  };

  return (
    <div
      ref={wrapRef}
      onMouseDown={onBgMouseDown}
      onDragOver={(e) => e.preventDefault()}
      onDrop={onDrop}
      className="canvas-bg"
      style={{
        position: "relative",
        flex: 1,
        overflow: "hidden",
        background: "var(--bg)",
        backgroundSize: `${20 * view.k}px ${20 * view.k}px`,
        backgroundPosition: `${view.x}px ${view.y}px`,
        cursor: panning ? "grabbing" : "default",
        userSelect: "none",
      }}
    >
      <div
        style={{
          position: "absolute",
          top: 0,
          left: 0,
          transform: `translate(${view.x}px, ${view.y}px) scale(${view.k})`,
          transformOrigin: "0 0",
          width: 0,
          height: 0,
        }}
      >
        {/* Edges */}
        <svg
          style={{
            position: "absolute",
            overflow: "visible",
            pointerEvents: "none",
          }}
          width="4000"
          height="3000"
        >
          {workflow.edges.map((e) => {
            const a = workflow.nodes.find((n) => n.id === e.from);
            const b = workflow.nodes.find((n) => n.id === e.to);
            if (!a || !b) return null;
            const fromPort = portForFrom(a);
            const toPort = e.toPort ?? portForTo(b);
            const p1 = portWorld(a, fromPort);
            const p2 = portWorld(b, toPort);
            return (
              <EdgePath
                key={e.id}
                x1={p1.x}
                y1={p1.y}
                x2={p2.x}
                y2={p2.y}
                kind={e.kind}
                running={running}
                onClick={() => removeEdge(e.id)}
              />
            );
          })}
          {wire &&
            (() => {
              const a = workflow.nodes.find((n) => n.id === wire.fromId);
              if (!a) return null;
              const p = portWorld(a, wire.fromPort);
              const kind =
                hoverPort?.port === "model" || hoverPort?.port === "tools"
                  ? "attach"
                  : "flow";
              return (
                <EdgePath
                  x1={p.x}
                  y1={p.y}
                  x2={wire.x}
                  y2={wire.y}
                  kind={kind}
                  ghost
                />
              );
            })()}
        </svg>

        {/* Nodes */}
        {workflow.nodes.map((n) => (
          <CanvasNode
            key={n.id}
            node={n}
            selected={selectedId === n.id}
            deployed={deployed}
            onMouseDown={(e) => startNodeDrag(e, n)}
            onStartWire={(e) => startWire(e, n.id, portForFrom(n))}
            onPortHover={(port) => setHoverPort({ nodeId: n.id, port })}
            onPortLeave={() => setHoverPort(null)}
            attachedSummary={attachedSummaries[n.id]}
          />
        ))}
      </div>

      {/* Controls */}
      <div
        style={{
          position: "absolute",
          bottom: 44,
          right: 16,
          zIndex: 4,
          display: "flex",
          flexDirection: "column",
          gap: 4,
          background: "var(--bg-elev-2)",
          border: "1px solid var(--border)",
          borderRadius: "var(--r-2)",
          padding: 4,
        }}
      >
        <button
          onClick={() => setView((v) => ({ ...v, k: Math.min(2, v.k * 1.15) }))}
          style={ctrlBtn}
        >
          +
        </button>
        <div
          style={{
            textAlign: "center",
            fontFamily: "var(--font-mono)",
            fontSize: 10,
            color: "var(--fg-dim)",
          }}
        >
          {Math.round(view.k * 100)}%
        </div>
        <button
          onClick={() =>
            setView((v) => ({ ...v, k: Math.max(0.3, v.k / 1.15) }))
          }
          style={ctrlBtn}
        >
          −
        </button>
        <div
          style={{ height: 1, background: "var(--border)", margin: "2px 0" }}
        />
        <button
          onClick={() => setView({ x: 40, y: 40, k: 0.95 })}
          style={ctrlBtn}
          title="Reset view"
        >
          ⊡
        </button>
      </div>

      {/* Hints */}
      <div
        style={{
          position: "absolute",
          bottom: 44,
          left: 16,
          zIndex: 4,
          display: "flex",
          gap: 12,
          alignItems: "center",
          fontFamily: "var(--font-mono)",
          fontSize: 10,
          color: "var(--fg-dim)",
        }}
      >
        {[
          ["drag bg", "pan"],
          ["scroll", "zoom"],
          ["drag port", "connect"],
          ["click edge", "delete"],
        ].map(([k, v]) => (
          <span key={k}>
            <span
              style={{
                display: "inline-flex",
                alignItems: "center",
                justifyContent: "center",
                minWidth: 18,
                height: 18,
                padding: "0 4px",
                borderRadius: 4,
                border: "1px solid var(--border-strong)",
                background: "var(--bg-elev-1)",
                fontFamily: "var(--font-mono)",
                fontSize: 10,
                color: "var(--fg-muted)",
              }}
            >
              {k}
            </span>{" "}
            {v}
          </span>
        ))}
      </div>

      {workflow.nodes.length === 0 && (
        <div
          style={{
            position: "absolute",
            inset: 0,
            display: "flex",
            flexDirection: "column",
            alignItems: "center",
            justifyContent: "center",
            pointerEvents: "none",
            gap: 12,
          }}
        >
          <div
            style={{
              width: 64,
              height: 64,
              borderRadius: 12,
              border: "1px dashed var(--border-strong)",
              display: "inline-flex",
              alignItems: "center",
              justifyContent: "center",
              color: "var(--fg-dim)",
              fontSize: 24,
            }}
          >
            +
          </div>
          <div
            style={{
              fontFamily: "var(--font-mono)",
              fontSize: 12,
              color: "var(--fg-dim)",
            }}
          >
            empty canvas · drag a trigger from the left to begin
          </div>
        </div>
      )}
    </div>
  );
}

// ── Edge path ──────────────────────────────────────────────────────────────
function EdgePath({
  x1,
  y1,
  x2,
  y2,
  kind,
  running,
  ghost,
  onClick,
}: {
  x1: number;
  y1: number;
  x2: number;
  y2: number;
  kind?: string;
  running?: boolean;
  ghost?: boolean;
  onClick?: () => void;
}) {
  const isAttach = kind === "attach";
  const color = isAttach ? "#E879F9" : "var(--accent)";
  let d: string;
  if (isAttach) {
    const off = Math.max(30, Math.abs(y2 - y1) * 0.45);
    d = `M ${x1} ${y1} C ${x1} ${y1 - off}, ${x2} ${y2 + off}, ${x2} ${y2}`;
  } else {
    const off = Math.max(40, Math.abs(x2 - x1) * 0.4);
    d = `M ${x1} ${y1} C ${x1 + off} ${y1}, ${x2 - off} ${y2}, ${x2} ${y2}`;
  }

  return (
    <g style={{ pointerEvents: ghost ? "none" : "auto" }}>
      <path
        d={d}
        fill="none"
        stroke="transparent"
        strokeWidth="14"
        onClick={onClick}
        style={{ cursor: "pointer" }}
      />
      <path
        d={d}
        fill="none"
        stroke={color}
        strokeWidth={1.5}
        strokeDasharray={isAttach ? "4 3" : ghost ? "3 4" : undefined}
        opacity={ghost ? 0.6 : 0.78}
      />
      {running && !ghost && (
        <circle r="3" fill={color}>
          <animateMotion dur="1.4s" repeatCount="indefinite" path={d} />
        </circle>
      )}
      <circle cx={x2} cy={y2} r="3" fill={color} opacity="0.95" />
    </g>
  );
}

const ctrlBtn: React.CSSProperties = {
  width: 28,
  height: 28,
  display: "flex",
  alignItems: "center",
  justifyContent: "center",
  background: "transparent",
  border: "none",
  color: "var(--fg-muted)",
  cursor: "pointer",
  fontFamily: "var(--font-mono)",
  fontSize: 16,
  borderRadius: "var(--r-1)",
};
