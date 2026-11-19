import { DataPolicyCard } from "./data_policy";
import type { Act } from "./nodes";
export function TrafficHistorySettings({
  module,
  act,
}: {
  module: string;
  act: Act;
}) {
  return (
    <DataPolicyCard
      module={module}
      kind="traffic"
      title={
        (module === "xray"
          ? "Xray"
          : module === "warp"
            ? "WARP"
            : "WireGuard") + " 流量"
      }
      act={act}
    />
  );
}
