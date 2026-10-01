"use client";

export interface DonutSegment {
  label: string;
  value: number;
  color: string;
}

// The centre figure's font size, fitted to the hole. A fixed size was
// proportionally 40% larger on the phone's smaller ring and had no bound on
// width, so a big enough spend ran across the ring. The cap (12% of the ring)
// keeps today's desktop size; the second term shrinks a long figure to fit
// 78% of the hole, taking a digit as 0.58em wide in the sans face.
export function centreFontSize(
  label: string,
  size: number,
  thickness: number,
): number {
  const hole = size - 2 * thickness;
  return Math.min(size * 0.12, (hole * 0.78) / (label.length * 0.58));
}

// Minimal SVG donut. Segments render as dash-array arcs on stacked circles,
// rotated so 0 starts at 12 o'clock. Legend is rendered by the parent.
export function Donut({
  segments,
  size = 168,
  thickness = 22,
  centerLabel,
  centerSub,
  ariaLabel,
}: {
  segments: DonutSegment[];
  size?: number;
  thickness?: number;
  centerLabel?: string;
  centerSub?: string;
  /** Read instead of the drawing, e.g. the exact amount a compact label rounds. */
  ariaLabel?: string;
}) {
  const total = segments.reduce((s, x) => s + x.value, 0) || 1;
  const r = (size - thickness) / 2;
  const c = 2 * Math.PI * r;
  const cx = size / 2;
  const labelSize = centerLabel
    ? centreFontSize(centerLabel, size, thickness)
    : 0;
  // Cumulative start fraction per segment, precomputed -- mutating a shared
  // accumulator from inside the map callback reassigns render-scope state.
  const starts: number[] = [];
  for (let a = 0, i = 0; i < segments.length; i++) {
    starts.push(a);
    a += segments[i].value / total;
  }

  return (
    <svg
      // The viewBox keeps the drawing's coordinate system at `size`, so none of
      // the geometry below changes. What changes is the rendered box: it was a
      // hard width={size}, so on a 343px column the donut stayed 168px wide and
      // pushed its own legend onto the next line.
      //
      // maxWidth pins it at `size` on desktop -- so nothing there moves -- while
      // aspectRatio keeps it square as it shrinks.
      viewBox={`0 0 ${size} ${size}`}
      role={ariaLabel ? "img" : undefined}
      aria-label={ariaLabel}
      style={{
        display: "block",
        width: "100%",
        maxWidth: size,
        // It is a flex item next to its legend. Without this the auto min-width
        // floor keeps it at `size` and pushes the legend onto its own line,
        // which is the wrap you see at 375px.
        minWidth: 0,
        height: "auto",
        aspectRatio: "1 / 1",
      }}
    >
      <g transform={`rotate(-90 ${cx} ${cx})`}>
        <circle
          cx={cx}
          cy={cx}
          r={r}
          fill="none"
          stroke="var(--border-soft)"
          strokeWidth={thickness}
        />
        {segments.map((s, i) => {
          const dash = (s.value / total) * c;
          return (
            <circle
              key={i}
              cx={cx}
              cy={cx}
              r={r}
              fill="none"
              stroke={s.color}
              strokeWidth={thickness}
              strokeDasharray={`${dash} ${c - dash}`}
              strokeDashoffset={-starts[i] * c}
            />
          );
        })}
      </g>
      {centerLabel && (
        <text
          x={cx}
          y={centerSub ? cx - 2 : cx + labelSize * 0.35}
          textAnchor="middle"
          fontFamily="var(--font-sans)"
          fontSize={labelSize}
          fontWeight="500"
          fill="var(--fg)"
        >
          {centerLabel}
        </text>
      )}
      {centerSub && (
        <text
          x={cx}
          y={cx + Math.max(12, labelSize * 0.7)}
          textAnchor="middle"
          fontFamily="var(--font-mono)"
          fontSize="9"
          fill="var(--fg-dim)"
          letterSpacing="0.06em"
        >
          {centerSub}
        </text>
      )}
    </svg>
  );
}
