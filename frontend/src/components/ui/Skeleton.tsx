import { Card } from "./index";

// Placeholders shaped like the content that is coming.
//
// Replacing "loading workflows…" with these is not decoration. A centred line
// of text occupies almost none of the space the list will need, so when the
// data lands the page jumps: the header shifts, anything the user was reaching
// for moves, and on a phone a tap already in flight can land on the wrong
// thing. A skeleton the same shape and height as the real rows makes the
// arrival a fill rather than a relayout.
//
// It also answers a question a spinner does not: a spinner says "something is
// happening", a skeleton says "a list of about this many things is happening".

/** One shimmering block. Width and height are the caller's business. */
export function Skeleton({
  width,
  height,
  radius = "var(--r-1)",
  style,
}: {
  width?: number | string;
  height?: number | string;
  radius?: string;
  style?: React.CSSProperties;
}) {
  return (
    <div
      className="skeleton"
      style={{ width, height, borderRadius: radius, ...style }}
    />
  );
}

/**
 * Stand-in for the workflow list while it loads.
 *
 * Three, not one and not ten: enough to read as a list rather than a single
 * stray box, few enough that a short real list does not shrink the page when it
 * arrives. The blocks are aria-hidden with one live status above them, so a
 * screen reader is told "loading" once instead of reading out nine meaningless
 * rectangles.
 */
export function WorkflowListSkeleton({ count = 3 }: { count?: number }) {
  return (
    <div>
      <span
        role="status"
        aria-live="polite"
        style={{
          position: "absolute",
          width: 1,
          height: 1,
          overflow: "hidden",
          clip: "rect(0 0 0 0)",
          whiteSpace: "nowrap",
        }}
      >
        Loading workflows
      </span>
      <div
        aria-hidden="true"
        style={{ display: "flex", flexDirection: "column", gap: 12 }}
      >
        {Array.from({ length: count }, (_, i) => (
          <Card key={i} style={{ padding: 16 }}>
            {/* A title line, then a meta row -- the same two-part shape a real
                workflow card has, so little moves when one replaces it. */}
            <Skeleton width="44%" height={14} />
            <div style={{ display: "flex", gap: 24, marginTop: 16 }}>
              <Skeleton width={72} height={11} />
              <Skeleton width={56} height={11} />
              <Skeleton width={64} height={11} />
            </div>
          </Card>
        ))}
      </div>
    </div>
  );
}
