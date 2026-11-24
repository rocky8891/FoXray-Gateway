import type { SVGProps } from "react";
export function CrossZoneIcon({size=20,...props}:SVGProps<SVGSVGElement>&{size?:number}){
 return <svg width={size} height={size} viewBox="0 0 1024 1024" fill="currentColor" aria-hidden="true" data-icon="CrossZoneIcon" {...props}><path d="M832 544A288 288 0 0 0 544 256V128A416 416 0 0 1 960 544h-128z m-576 0A288 288 0 0 0 544 832v128A416 416 0 0 1 128 544h128zM576 768a192 192 0 1 1 384 0 192 192 0 0 1-384 0z m192-48a48 48 0 1 0 0 96 48 48 0 0 0 0-96zM298.688 192a106.688 106.688 0 1 0 0 213.312 106.688 106.688 0 0 0 0-213.312zM64 298.688a234.688 234.688 0 1 1 469.312 0 234.688 234.688 0 0 1-469.312 0z"/></svg>;
}
