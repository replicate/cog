import { StatusBadge } from "@/components/StatusBadge";
import type { HealthResponse } from "@/types/health";

export function SetupPanel({ setup }: { setup: HealthResponse["setup"] }) {
  if (!setup) return null;
  return (
    <details id="setup-panel">
      <summary>
        Setup <StatusBadge status={setup.status} />
      </summary>
      {/* oxlint-disable-next-line jsx-a11y/no-noninteractive-tabindex -- scrollable logs must be keyboard reachable */}
      <pre id="setup-logs" aria-label="Setup logs" tabIndex={0}>
        {setup.logs}
      </pre>
    </details>
  );
}
