import { CopyButton } from "./resource_controls";
import { PageHeader } from "./ui";
import type { ReactNode } from "react";

export function LogDetailHeading({ title, copyText, icon, description }: {
  title: string; copyText?: string; icon?: ReactNode; description?: ReactNode;
}) {
  return <div className="log-detail-heading"><PageHeader title={title} icon={icon} description={description}
    actions={copyText === undefined ? undefined : <CopyButton text={copyText}/>} /></div>;
}
