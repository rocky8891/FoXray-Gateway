import { forwardRef, useId, useImperativeHandle, useLayoutEffect, useRef, useState, type HTMLAttributes } from "react";
import { ChevronDown, ChevronUp } from "lucide-react";

export const MenuScrollArea = forwardRef<HTMLDivElement, HTMLAttributes<HTMLDivElement>>(function MenuScrollArea({ children, className = "", ...props }, forwardedRef) {
  const content = useRef<HTMLDivElement>(null), track = useRef<HTMLDivElement>(null);
  const drag = useRef<{ y: number; top: number } | null>(null);
  const [size, setSize] = useState({ top: 0, max: 0, viewport: 0, total: 0, track: 0 });
  const generatedId = useId(), id = props.id || generatedId;
  useImperativeHandle(forwardedRef, () => content.current!, []);
  const measure = () => {
    const element = content.current;
    if (!element) return;
    const next = { top: element.scrollTop, max: Math.max(0, element.scrollHeight - element.clientHeight), viewport: element.clientHeight, total: element.scrollHeight, track: track.current?.clientHeight || 0 };
    setSize(old => Object.keys(next).every(key => old[key as keyof typeof old] === next[key as keyof typeof next]) ? old : next);
  };
  useLayoutEffect(() => {
    const element = content.current;
    if (!element) return;
    const resize = new ResizeObserver(measure);
    resize.observe(element);
    if (track.current) resize.observe(track.current);
    const mutation = new MutationObserver(measure);
    mutation.observe(element, { childList: true, subtree: true, characterData: true });
    measure();
    return () => { resize.disconnect(); mutation.disconnect(); };
  }, [children, size.max > 0]);
  const scroll = (delta: number) => { if (content.current) content.current.scrollTop += delta; };
  const thumbHeight = Math.min(size.track, Math.max(24, size.track * size.viewport / Math.max(1, size.total)));
  const thumbTop = size.max ? size.top / size.max * Math.max(0, size.track - thumbHeight) : 0;

  return <div className={"menu-scroll-area" + (size.max ? " has-scrollbar" : "")}>
    <div {...props} id={id} ref={content} className={"menu-scroll-content " + className} onScroll={event => { measure(); props.onScroll?.(event); }}>{children}</div>
    {size.max > 0 && <div className="menu-scrollbar">
      <button type="button" className="menu-scroll-arrow" tabIndex={-1} aria-label="向上滚动列表" disabled={size.top <= 0}
        onPointerDown={event => event.preventDefault()} onClick={() => scroll(-40)}><ChevronUp size={12}/></button>
      <div ref={track} className="menu-scroll-track" role="scrollbar" tabIndex={0} aria-label="列表滚动条"
        aria-controls={id} aria-orientation="vertical" aria-valuemin={0} aria-valuemax={Math.round(size.max)} aria-valuenow={Math.round(size.top)}
        onKeyDown={event => {
          const delta = ({ ArrowDown: 40, ArrowUp: -40, PageDown: size.viewport * 0.85, PageUp: -size.viewport * 0.85, Home: -size.max, End: size.max } as Record<string, number>)[event.key];
          if (delta !== undefined) { event.preventDefault(); event.stopPropagation(); scroll(delta); }
        }}
        onPointerDown={event => {
          if (event.target !== event.currentTarget || event.button !== 0) return;
          event.preventDefault();
          const y = event.clientY - event.currentTarget.getBoundingClientRect().top;
          scroll((y < thumbTop ? -1 : 1) * size.viewport * 0.85);
        }}>
        <div className="menu-scroll-thumb" style={{ height: thumbHeight, top: thumbTop }}
          onPointerDown={event => {
            if (event.button !== 0) return;
            event.preventDefault(); event.stopPropagation();
            drag.current = { y: event.clientY, top: content.current?.scrollTop || 0 };
            event.currentTarget.setPointerCapture(event.pointerId);
          }}
          onPointerMove={event => {
            if (!drag.current || !content.current) return;
            content.current.scrollTop = drag.current.top + (event.clientY - drag.current.y) * size.max / Math.max(1, size.track - thumbHeight);
          }}
          onPointerUp={event => { drag.current = null; if (event.currentTarget.hasPointerCapture(event.pointerId)) event.currentTarget.releasePointerCapture(event.pointerId); }}
          onLostPointerCapture={() => { drag.current = null; }}/>
      </div>
      <button type="button" className="menu-scroll-arrow" tabIndex={-1} aria-label="向下滚动列表" disabled={size.top >= size.max}
        onPointerDown={event => event.preventDefault()} onClick={() => scroll(40)}><ChevronDown size={12}/></button>
    </div>}
  </div>;
});
