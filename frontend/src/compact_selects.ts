import { useLayoutEffect } from "react";
const selector =
  ".overview-page :is(.overview-heading-row,.chart-title-controls) select, select.firewall-driver-select";
export function useCompactSelects() {
  useLayoutEffect(() => {
    const context = document.createElement("canvas").getContext("2d");
    if (!context) return;
    let pending = 0,
      disposed = false;
    let tracked = new Set<HTMLSelectElement>();
    const cache = new WeakMap<HTMLSelectElement, string>();
    const measure = () => {
      pending = 0;
      if (disposed) return;
      const current = new Set(
        document.querySelectorAll<HTMLSelectElement>(selector),
      );
      for (const el of tracked)
        if (!current.has(el)) {
          el.removeAttribute("data-compact-select");
          el.style.removeProperty("--select-content-width");
        }
      for (const el of current) {
        const style = getComputedStyle(el),
          fitOptions = el.classList.contains("firewall-driver-select"),
          labels = fitOptions ? Array.from(el.options, option => option.label) : [el.selectedOptions[0]?.label || ""],
          font = `${style.fontStyle} ${style.fontWeight} ${style.fontSize} ${style.fontFamily}`,
          gap = parseFloat(style.letterSpacing) || 0,
          padding = fitOptions ? parseFloat(style.paddingLeft) + parseFloat(style.paddingRight) + 8 : 32,
          key = font + "|" + gap + "|" + padding + "|" + labels.join("\n");
        el.dataset.compactSelect = "true";
        if (cache.get(el) === key) continue;
        context.font = font;
        const width = Math.ceil(
          Math.max(0, ...labels.map(label => context.measureText(label).width + (fitOptions ? Math.max(0, gap) * label.length : 0))) +
            padding +
            parseFloat(style.borderLeftWidth || "0") +
            parseFloat(style.borderRightWidth || "0"),
        );
        el.style.setProperty(
          "--select-content-width",
          Math.max(48, width) + "px",
        );
        cache.set(el, key);
      }
      tracked = current;
    };
    const schedule = () => {
      if (!disposed && !pending) pending = requestAnimationFrame(measure);
    };
    const observer = new MutationObserver(schedule);
    observer.observe(document.body, {
      childList: true,
      subtree: true,
      characterData: true,
    });
    document.addEventListener("change", schedule, true);
    window.addEventListener("resize", schedule);
    document.fonts.ready.then(schedule);
    measure();
    return () => {
      disposed = true;
      cancelAnimationFrame(pending);
      observer.disconnect();
      document.removeEventListener("change", schedule, true);
      window.removeEventListener("resize", schedule);
      for (const el of tracked) {
        el.removeAttribute("data-compact-select");
        el.style.removeProperty("--select-content-width");
      }
    };
  }, []);
}
