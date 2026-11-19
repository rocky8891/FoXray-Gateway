import type { SVGProps } from "react";
// Compact vector interpretation of the supplied round, burgundy-haired avatar.
export function KomariIcon({
  size = 20,
  ...props
}: SVGProps<SVGSVGElement> & { size?: number }) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 64 64"
      aria-hidden="true"
      data-icon="komari"
      {...props}
    >
      <circle
        cx="32"
        cy="32"
        r="30"
        fill="#29372f"
        stroke="currentColor"
        strokeWidth="2"
      />
      <path
        d="M7 33C6 16 17 5 32 5s27 11 26 28c0 12-11 24-26 26C17 57 7 47 7 33Z"
        fill="#8f4052"
      />
      <path
        d="M12 33c2-10 8-17 13-20 0 9 7 16 17 18l6-9c5 6 7 14 5 22-4 9-12 13-21 13-11 0-19-6-21-15Z"
        fill="#fff5e6"
        stroke="#29372f"
        strokeWidth="2.5"
        strokeLinejoin="round"
      />
      <path
        d="M15 16c5-7 12-9 19-8 8 0 16 5 19 13-6-1-10-4-14-8 3 7 4 13 1 20-11-2-17-8-17-16-3 6-7 11-12 13Z"
        fill="#9d485e"
      />
      <path
        d="M15 14c6-6 13-8 22-6"
        fill="none"
        stroke="#cf6a80"
        strokeWidth="4"
        strokeLinecap="round"
      />
      <ellipse cx="22" cy="38" rx="7" ry="8" fill="#29372f" />
      <ellipse cx="43" cy="38" rx="7" ry="8" fill="#29372f" />
      <ellipse cx="22" cy="41" rx="4.5" ry="3.5" fill="#e7cc75" />
      <ellipse cx="43" cy="41" rx="4.5" ry="3.5" fill="#e7cc75" />
      <circle cx="20.5" cy="34.5" r="2.5" fill="white" />
      <circle cx="41.5" cy="34.5" r="2.5" fill="white" />
      <path d="M29 48q3-3 6 0c0 6-6 6-6 0Z" fill="#e68b63" />
      <path
        d="m23 58 9-6 9 6"
        fill="#fff5e6"
        stroke="#29372f"
        strokeWidth="2.5"
        strokeLinejoin="round"
      />
      <path d="m28 57 4-3 4 3-4 4Z" fill="#a94a59" />
    </svg>
  );
}
