import type { HealthResponse } from "../../domain/types";
import { StatusBadge } from "../../components/StatusBadge";

export function SetupPanel({ setup }: { setup: HealthResponse["setup"] }) {
  if (!setup) return null;
  return (
    <details id="setup-panel">
      <summary>
        Setup <StatusBadge status={setup.status} />
      </summary>
      <pre id="setup-logs">{setup.logs}</pre>
    </details>
  );
}
