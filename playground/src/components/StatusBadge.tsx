import { Badge, type BadgeVariant } from "@cloudflare/kumo/components/badge";

export function StatusBadge({ status }: { status?: string }) {
  const normalized = status?.toLowerCase() || "unknown";
  const variants: Record<string, BadgeVariant> = {
    ready: "success",
    succeeded: "success",
    busy: "warning",
    starting: "warning",
    processing: "warning",
    failed: "error",
    error: "error",
    unreachable: "error",
    canceled: "secondary",
    unknown: "secondary",
  };
  return (
    <Badge variant={variants[normalized] ?? "secondary"} appearance="dot">
      {normalized}
    </Badge>
  );
}
