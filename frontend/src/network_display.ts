const builtins: Record<string, string> = { "0":"unspec", "253":"default", "254":"main", "255":"local" };
export function routeTableId(value: unknown): string {
  const raw = String(value ?? "254");
  return Object.keys(builtins).find(key => builtins[key] === raw) || raw;
}
export function routeTableLabel(value: unknown): string {
  const id = routeTableId(value);
  return builtins[id] ? `${id} · ${builtins[id]}` : id;
}
export function remainingPlanSeconds(expires: number, now: number): number {
  return Number.isFinite(expires) && Number.isFinite(now) ? Math.max(0, Math.ceil(expires - now)) : 0;
}
