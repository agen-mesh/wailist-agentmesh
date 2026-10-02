import { MIN_CANVAS } from "./panelSizing";

// What the studio shows at a given width, on a pointer client.
//
// The studio used to pick between three columns and a bottom sheet by width
// alone, so a laptop window dragged narrow lost both side panels. Here they
// collapse to a 26px rail instead: a rail is a button, so there is always
// something to click to get the panel back. Two rails plus MIN_CANVAS fit in
// 372px.
//
// Pure, like panelSizing.ts beside it, so the rules are testable without a DOM.

/** Width of a collapsed panel: the strip carrying its expand button. */
export const RAIL_W = 26;

export interface StudioFitInput {
  /** Measured width of the panel row. 0 or NaN = not measured yet. */
  rowWidth: number;
  paletteW: number;
  inspectorW: number;
  /** Reader has not collapsed the palette, and is allowed to see one. */
  paletteWanted: boolean;
  /** Reader has not collapsed the chat/inspector rail. */
  inspectorWanted: boolean;
  /**
   * The panel the reader last explicitly opened from its rail, if any.
   *
   * Only consulted when both are wanted and both will not fit. It is a
   * preference, not a collapse: the moment there is room for both, both come
   * back. Expanding used to mark the OTHER panel collapsed instead, which is
   * reader-intent state -- so widening the window afterwards left it a rail
   * forever, though nobody had asked to close it.
   */
  prefer?: "palette" | "inspector" | null;
}

export interface StudioFit {
  palette: boolean;
  inspector: boolean;
  /** True when the window, not the reader, closed something. */
  forced: boolean;
}

function widthOf(open: boolean, w: number): number {
  return open ? w : RAIL_W;
}

function fits(
  rowWidth: number,
  palette: boolean,
  inspector: boolean,
  paletteW: number,
  inspectorW: number,
): boolean {
  return (
    widthOf(palette, paletteW) + widthOf(inspector, inspectorW) + MIN_CANVAS <=
    rowWidth
  );
}

export function fitPanels(input: StudioFitInput): StudioFit {
  const {
    rowWidth,
    paletteW,
    inspectorW,
    paletteWanted,
    inspectorWanted,
    prefer = null,
  } = input;

  // Unmeasured: the reader's choice stands until the ResizeObserver lands.
  if (!Number.isFinite(rowWidth) || rowWidth <= 0) {
    return {
      palette: paletteWanted,
      inspector: inspectorWanted,
      forced: false,
    };
  }

  // At most one panel open: the reader gets it, whatever the width. MIN_CANVAS
  // guards the space BETWEEN two panels; it is not a veto on opening one. This
  // clause is what stops the rails being buttons that do nothing at 520px.
  if (!paletteWanted || !inspectorWanted) {
    return {
      palette: paletteWanted,
      inspector: inspectorWanted,
      forced: false,
    };
  }

  if (fits(rowWidth, true, true, paletteW, inspectorW)) {
    return { palette: true, inspector: true, forced: false };
  }

  // The reader opened one from its rail. It opens, whatever the width -- same
  // reasoning as the single-panel case above: MIN_CANVAS guards the space
  // between two panels, not the right to open one. Without this a 520px row
  // refused both, so the rails were buttons that did nothing.
  if (prefer) {
    return {
      palette: prefer === "palette",
      inspector: prefer === "inspector",
      forced: true,
    };
  }

  // No preference yet -- opening the studio narrow shows the graph, and the
  // rails are there to ask for the rest.
  if (fits(rowWidth, false, true, paletteW, inspectorW)) {
    return { palette: false, inspector: true, forced: true };
  }
  return { palette: false, inspector: false, forced: true };
}
