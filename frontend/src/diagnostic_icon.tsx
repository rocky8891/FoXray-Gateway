import type { SVGProps } from "react";
export function DiagnosticIcon({
  size = 18,
  ...props
}: SVGProps<SVGSVGElement> & { size?: number }) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 1024 1024"
      fill="currentColor"
      aria-hidden="true"
      data-icon="DiagnosticIcon"
      {...props}
    >
      <path d="M128 64a64 64 0 0 0-64 64v768a64 64 0 0 0 64 64h768a64 64 0 0 0 64-64V128a64 64 0 0 0-64-64H128z m0-64h768a128 128 0 0 1 128 128v768a128 128 0 0 1-128 128H128a128 128 0 0 1-128-128V128a128 128 0 0 1 128-128z" />
      <path d="M185.024 544H24.704v-64h176.128a38.4 38.4 0 0 1 34.368 21.248l63.168 126.336 116.608-427.52a44.8 44.8 0 0 1 87.296 3.84l96.64 540.8 128.896-368.32a44.8 44.8 0 0 1 82.368-5.248l54.4 108.864H998.4v64h-149.632a38.4 38.4 0 0 1-34.368-21.248l-41.216-82.368-138.048 394.56a44.8 44.8 0 0 1-86.4-6.912L454.272 299.136 346.88 692.928a44.8 44.8 0 0 1-83.264 8.192L184.96 544z" />
    </svg>
  );
}
