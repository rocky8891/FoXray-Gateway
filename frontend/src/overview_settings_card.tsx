import type { ReactNode } from "react";
import { PageHeader, Panel, SettingsSection } from "./ui";

export function OverviewSettingsCard({
  title,
  info,
  header,
  children,
  actions,
}: {
  title: string;
  info?: ReactNode;
  header?: ReactNode;
  children: ReactNode;
  actions?: ReactNode;
}) {
  return (
    <Panel className="overview-settings-card">
      {header || <PageHeader title={title} description={info} />}
      <SettingsSection className="overview-settings-body">
        {children}
      </SettingsSection>
      {actions}
    </Panel>
  );
}
