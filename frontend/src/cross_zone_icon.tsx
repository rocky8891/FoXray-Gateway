import type { SVGProps } from "react";
export function CrossZoneIcon({size=20,...props}:SVGProps<SVGSVGElement>&{size?:number}){
 return <svg width={size} height={size} viewBox="0 0 1024 1024" fill="currentColor" aria-hidden="true" data-icon="CrossZoneIcon" {...props}><path d="M832 544A288 288 0 0 0 544 256V128A416 416 0 0 1 960 544h-128z m-576 0A288 288 0 0 0 544 832v128A416 416 0 0 1 128 544h128zM288 384a96 96 0 1 0 0-192 96 96 0 0 0 0 192z m0 128a224 224 0 1 0 0-448 224 224 0 0 0 0 448zM768 832a64 64 0 1 0 0-128 64 64 0 0 0 0 128z m0 128a192 192 0 1 0 0-384 192 192 0 0 0 0 384z"/></svg>;
}
