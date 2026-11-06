/**
 * Studio's share bridge was not part of the committed console. A no-op keeps
 * the page from posting state to window.parent. Tracing owns the one parent
 * channel, and it pins the target origin.
 */
export function installShareSync(): void {}
