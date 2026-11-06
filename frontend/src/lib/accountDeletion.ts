export function clearDeletedAccountStorage(): void {
  const exact = new Set([
    "agentmesh_checkout_phone",
    "agentmesh_credits_v1",
  ]);
  const keys = Object.keys(window.localStorage).filter((key) =>
    exact.has(key) || key.startsWith("agentmesh_lastrun_") ||
    key.startsWith("agentmesh_settlements_") || key.startsWith("agentmesh_helixbox_sessions_"),
  );
  for (const key of keys) window.localStorage.removeItem(key);
}
