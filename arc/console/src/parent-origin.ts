/** Arc Studio's origin. Used when this page is not framed and has no referrer. */
export const STUDIO_ORIGIN = 'https://studio.arc.io'

/**
 * Origin allowed to receive trace and console events, and to enable tracing.
 * Never '*': a wildcard target delivers the message, including signed
 * typed-data, to whatever frame embedded this page.
 */
export function parentOrigin(): string {
  try {
    if (window.location.ancestorOrigins?.length) {
      return window.location.ancestorOrigins[0]
    }
  } catch {
    /* sandboxed iframe may throw */
  }

  try {
    if (document.referrer) {
      return new URL(document.referrer).origin
    }
  } catch {
    /* malformed referrer */
  }

  return STUDIO_ORIGIN
}
