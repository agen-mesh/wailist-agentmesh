// The order two independent requests answered in.
//
// Wall-clock stamps cannot express it. Date.now() has millisecond resolution,
// and two responses that land back to back routinely read the same value --
// measured at 10,000 ties in 10,000 back-to-back pairs -- so any comparison
// of them has to break the tie arbitrarily, and whichever side loses can be
// suppressed by data that is actually older.
//
// A counter has no ties. Every call returns a value greater than the last, so
// comparing two of them recovers the order the answers arrived in, which is
// the only thing a reader of two views of the same object needs to know.
let landed = 0;

/** The next position in the order responses landed. Never repeats. */
export function nextResponseSeq(): number {
  return ++landed;
}
