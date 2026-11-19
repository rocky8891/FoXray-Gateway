// Shared left gutter keeps stacked charts aligned without excessive whitespace.
export const CHART_AXIS_WIDTH = 48;
export const CHART_MARGIN = { top: 14, right: 20, bottom: 5, left: 0 };

export function AlignedAxisTick({
  y = 0,
  payload,
  format = String,
}: {
  y?: number;
  payload?: { value?: number };
  format?: (value: number) => string;
}) {
  const raw = format(Number(payload?.value ?? 0));
  const match = raw.match(/^(-?[\d.]+)\s*(.*)$/);
  let amount = match ? Number(match[1]) : Number(payload?.value ?? 0),
    unit = match?.[2] || "";
  if (Math.abs(amount) >= 1000) {
    const units = ["B/s", "KB/s", "MB/s", "GB/s", "TB/s"];
    const index = units.indexOf(unit);
    if (index >= 0 && index < units.length - 1) {
      amount /= 1000;
      unit = units[index + 1];
    } else if (unit === "ms") {
      amount /= 1000;
      unit = "s";
    } else if (!unit) {
      amount /= 1000;
      unit = "k";
    }
  }
  while (Math.abs(amount) >= 1000) {
    amount /= 1000;
    unit =
      unit === "k" ? "M" : unit === "M" ? "G" : unit === "G" ? "T" : "k" + unit;
  }
  let number = String(Number(amount.toPrecision(3)));
  if (number.length > 4)
    number = amount.toFixed(Math.abs(amount) >= 100 ? 0 : 1);
  if (number.length > 4) {
    number = (amount / 1000).toPrecision(2);
    unit = "k" + unit;
  }
  const percent = unit === "%";
  const multiline = !!unit && !percent;
  const anchorX = 14;
  return (
    <g
      transform={`translate(${anchorX},${y})`}
      fill="var(--muted)"
      textAnchor="middle"
      className="aligned-axis-tick"
    >
      <text y={multiline ? -5 : 0} dominantBaseline="central" fontSize={11}>
        {number}
        {percent ? "%" : ""}
      </text>
      {multiline && (
        <text
          y={6}
          dominantBaseline="central"
          fontSize={9}
          fill="var(--axis-unit, #748090)"
        >
          {unit}
        </text>
      )}
    </g>
  );
}
export function TimeAxisTick({
  x = 0,
  y = 0,
  payload,
  index = 0,
  visibleTicksCount = 0,
}: {
  x?: number;
  y?: number;
  payload?: { value?: number };
  index?: number;
  visibleTicksCount?: number;
}) {
  const label = new Date(Number(payload?.value || 0) * 1000).toLocaleTimeString(
    "zh-CN",
    { hour: "2-digit", minute: "2-digit" },
  );
  return (
    <text
      x={x}
      y={y + 4}
      dominantBaseline="hanging"
      textAnchor={
        index === 0
          ? "start"
          : index === (visibleTicksCount || 5) - 1
            ? "end"
            : "middle"
      }
      fill="var(--muted)"
      fontSize={11}
    >
      {label}
    </text>
  );
}
export function timeTicks(domain?: [number, number]) {
  return domain && domain[1] > domain[0]
    ? Array.from(
        { length: 5 },
        (_, i) => domain[0] + ((domain[1] - domain[0]) * i) / 4,
      )
    : undefined;
}

export function formatAxisTime(v: number) {
  return new Date(v * 1000).toLocaleTimeString("zh-CN", {
    hour: "2-digit",
    minute: "2-digit",
  });
}
