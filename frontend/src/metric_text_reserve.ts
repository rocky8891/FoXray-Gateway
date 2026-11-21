// Stable representatives, never the current live value. Exceptional values use
// ellipsis and the full-value tooltip instead of resizing all metrics in a card.
export function metricValueReserve(key: string): string {
  if(key==="upload"||key==="download")return "888.88 MB/s";
  if(key==="latency")return "88888.8 ms";
  if(key==="connections")return "888888";
  if(key==="cpu"||key==="memory"||key==="root")return "100.0%";
  if(key==="load")return "888.88";
  return "8888.88";
}
