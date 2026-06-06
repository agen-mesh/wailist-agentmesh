"use client";
import { Pill } from "@/components/ui";
import { LOG_LINES } from "@/lib/data";

interface LogDrawerProps {
  open: boolean;
  onToggle: () => void;
}

export function LogDrawer({ open, onToggle }: LogDrawerProps) {
  return (
    <div style={{
      position: "absolute", left: 0, right: 0, bottom: 0,
      background: "var(--bg-elev-1)", borderTop: "1px solid var(--border)",
      height: open ? 220 : 32,
      transition: "height .2s cubic-bezier(.2,.8,.2,1)",
      display: "flex", flexDirection: "column",
      zIndex: 5,
    }}>
      <div onClick={onToggle} style={{ height: 32, padding: "0 14px", display: "flex", alignItems: "center", justifyContent: "space-between", cursor: "pointer", borderBottom: open ? "1px solid var(--border)" : "none" }}>
        <div style={{ display: "flex", alignItems: "center", gap: 12 }}>
          <span style={{ fontFamily: "var(--font-mono)", fontSize: 10, color: "var(--fg-muted)", textTransform: "uppercase", letterSpacing: "0.08em" }}>console</span>
          <Pill mono tone="ok" dot>run · r-1842 · 2.3s</Pill>
          <Pill mono>0.001 ALGO spent</Pill>
        </div>
        <span style={{ fontFamily: "var(--font-mono)", fontSize: 11, color: "var(--fg-dim)" }}>
          {open ? "▾ collapse" : "▴ expand"}
        </span>
      </div>
      {open && (
        <div style={{ flex: 1, overflow: "auto", padding: "8px 14px", fontFamily: "var(--font-mono)", fontSize: 11, lineHeight: 1.65 }}>
          {LOG_LINES.map((l, i) => (
            <div key={i} style={{ display: "grid", gridTemplateColumns: "100px 60px 180px 1fr", gap: 12, color: "var(--fg-muted)" }}>
              <span style={{ color: "var(--fg-dim)" }}>{l.t}</span>
              <span style={{ color: l.lvl === "pay" ? "#E879F9" : l.lvl === "a2a" ? "var(--info)" : l.lvl === "ok" ? "var(--accent)" : "var(--fg-muted)" }}>
                {l.lvl.toUpperCase()}
              </span>
              <span>{l.src}</span>
              <span style={{ color: "var(--fg)" }}>{l.msg}</span>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
