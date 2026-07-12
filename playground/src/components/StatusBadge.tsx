import { Badge, type BadgeVariant } from "@cloudflare/kumo/components/badge";

/** Maps known Cog statuses to semantic colors and renders unknown values neutrally. */
export function StatusBadge({ status }: { status?: string }) {
  const normalized = status?.toLowerCase() || "unknown";
  const variants: Record<string, BadgeVariant> = {
    ready: "success",
    succeeded: "success",
    busy: "warning",
    starting: "warning",
    processing: "warning",
    defunct: "error",
    failed: "error",
    error: "error",
    setup_failed: "error",
    unhealthy: "error",
    unreachable: "error",
    canceled: "neutral",
    unknown: "neutral",
  };
  return (
    <output>
      <Badge variant={variants[normalized] ?? "neutral"} appearance="dot">
        {normalized}
      </Badge>
    </output>
  );
}
