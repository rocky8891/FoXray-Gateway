import { Fragment, isValidElement, type ReactNode } from "react";

// Keep author-provided lists intact; normalize strings and composed Field hints too.
function paragraphs(text: string): ReactNode {
  const parts = text.trim().split(/(?<=[。；])\s*|\n+/u).map(part=>part.trim()).filter(Boolean);
  if (!parts.length) return null;
  if (parts.length === 1) return <p>{text.trim()}</p>;
  return <ul>{parts.map((part,index)=><li key={index}>{part}</li>)}</ul>;
}
function flatten(node:ReactNode): ReactNode[] {
  if (Array.isArray(node)) return node.flatMap(flatten);
  if (isValidElement<{children?:ReactNode}>(node) && node.type===Fragment) return flatten(node.props.children);
  return node == null || typeof node==='boolean' ? [] : [node];
}
export function InfoContent({children}:{children:ReactNode}) {
  const nodes=flatten(children), groups:ReactNode[]=[];
  let inline:ReactNode[]=[];
  const flush=()=>{
    if (!inline.length) return;
    const current=inline;inline=[];
    groups.push(<Fragment key={groups.length}>{current.every(n=>typeof n==='string'||typeof n==='number')
      ? paragraphs(current.join('')) : <p>{current}</p>}</Fragment>);
  };
  for (const node of nodes) {
    if (isValidElement(node) && node.type==='br') {flush();continue;}
    if (isValidElement<{children?:ReactNode}>(node) && node.type==='p') {
      flush();groups.push(<Fragment key={groups.length}><InfoContent>{node.props.children}</InfoContent></Fragment>);
    } else if (isValidElement(node) && typeof node.type==='string' && ['ul','ol','dl','div','section','table','pre','h3','h4','blockquote'].includes(node.type)) {
      flush();groups.push(<Fragment key={groups.length}>{node}</Fragment>);
    } else inline.push(node);
  }
  flush();
  return <>{groups}</>;
}
