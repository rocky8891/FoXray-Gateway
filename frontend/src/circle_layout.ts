export function circleLayout(width: number, height: number, style: string, matchHeartDiameter = false) {
  const padding = 6,
    gap = Math.max(12, Math.min(width, height) * 0.055);
  const w = Math.max(1, width - padding * 2),
    h = Math.max(1, height - padding * 2);
  const cx = width / 2,
    cy = height / 2;
  const result = (
    diameter: number,
    centers: number[][],
    layout: string,
    columns = 2,
    diameters = [diameter, diameter, diameter, diameter],
  ) => {
    const sizes = diameters.map(size => Math.max(1, Math.floor(size)));
    return {
      diameter: Math.max(1, Math.floor(diameter)),
      diameters: sizes,
      centers: centers.map(([x, y], index) => {
        const radius = sizes[index] / 2;
        return [Math.round(x - radius) + radius, Math.round(y - radius) + radius];
      }),
      layout,
      columns,
    };
  };
  const line = (vertical: boolean) => {
    const major = vertical ? h : w,
      minor = vertical ? w : h;
    const d = Math.max(1, Math.min((major - gap * 3) / 4, minor));
    return result(
      d,
      Array.from({ length: 4 }, (_, i) =>
        vertical
          ? [cx, padding + (major * (i + 0.5)) / 4]
          : [padding + (major * (i + 0.5)) / 4, cy],
      ),
      vertical ? "single" : "ring-row",
      vertical ? 1 : 4,
    );
  };
  if (style === "radial-vertical") return line(true);
  if (style === "radial") return line(false);
  if (style === "radial-square") {
    const reference = matchHeartDiameter
      ? circleLayout(width, height, "radial-diamond-compact").diameter
      : Infinity;
    const d = Math.max(1, Math.min((w - gap) / 2, (h - gap) / 2, reference)),
      offset = (d + gap) / 2;
    return result(
      d,
      [
        [-1, -1],
        [1, -1],
        [-1, 1],
        [1, 1],
      ].map(([x, y]) => [cx + x * offset, cy + y * offset]),
      "grid",
    );
  }
  if (style === "radial-lightning" || style === "radial-reverse-lightning") {
    const reverse = style === "radial-reverse-lightning";
    const stagger = 0.44;
    const diameter = Math.max(1, Math.min((w - gap * 3) / 4, h / (1 + stagger)));
    const offset = diameter * stagger / 2;
    return result(
      diameter,
      Array.from({ length: 4 }, (_, index) => [
        padding + (w * (index + 0.5)) / 4,
        cy + ((index % 2 === 0) !== reverse ? offset : -offset),
      ]),
      reverse ? "reverse-lightning-row" : "lightning-row",
      4,
    );
  }
  if (style === "radial-wave") {
    const d = Math.max(1, Math.min((w - 3 * gap) / 4, h / 1.3));
    const amplitude = Math.min(d * 0.15, (h - d) / 2);
    return result(
      d,
      Array.from({ length: 4 }, (_, i) => [
        padding + (w * (i + 0.5)) / 4,
        cy + [0, -amplitude, amplitude, 0][i],
      ]),
      "wave-row",
      4,
    );
  }
  if (style === "radial-mixed") {
    const geometry = (big: number) => {
      const small = big * 0.62,
        dy = (big - small) / 2;
      const offset =
        Math.sqrt(Math.max(0, (small + gap) ** 2 - (2 * dy) ** 2)) / 2;
      const dx =
        offset +
        Math.sqrt(Math.max(0, ((big + small) / 2 + gap) ** 2 - dy ** 2));
      return { small, dy, offset, dx };
    };
    let low = 1,
      high = Math.min(w, h);
    for (let i = 0; i < 40; i++) {
      const d = (low + high) / 2,
        g = geometry(d);
      if (d + 2 * g.dx <= w) low = d;
      else high = d;
    }
    const g = geometry(low);
    return result(
      low,
      [
        [cx - g.dx, cy],
        [cx - g.offset, cy - g.dy],
        [cx + g.offset, cy + g.dy],
        [cx + g.dx, cy],
      ],
      "mixed",
      4,
      [low, g.small, g.small, low],
    );
  }
  // The shallow anchor in a top layout keeps the horizontal treatment requested earlier.
  if (style === "radial-diamond-compact" && w / h > 3) return line(false);
  const compact = style === "radial-diamond-compact";
  const areaWidth = Math.min(w, h * (compact ? 1.35 : 1.8));
  let low = 1,
    high = Math.min(areaWidth, h) / 2;
  for (let i = 0; i < 40; i++) {
    const d = (low + high) / 2,
      dx = (areaWidth - d) / 2,
      dy = (h - d) / 2;
    if (2 * dx >= d + gap && 2 * dy >= d + gap && Math.hypot(dx, dy) >= d + gap)
      low = d;
    else high = d;
  }
  const dx = (areaWidth - low) / 2,
    dy = (h - low) / 2;
  return result(
    low,
    [
      [cx - dx, cy],
      [cx, cy - dy],
      [cx, cy + dy],
      [cx + dx, cy],
    ],
    "diamond",
  );
}
