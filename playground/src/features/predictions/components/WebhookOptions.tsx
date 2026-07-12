import { Checkbox } from "@cloudflare/kumo/components/checkbox";

import { WEBHOOK_EVENTS } from "@/features/predictions/constants";
import type { WebhookEvent } from "@/features/predictions/types";

type Props = {
  value: WebhookEvent[];
  webhookBase: string;
  disabled?: boolean;
  onChange: (value: WebhookEvent[]) => void;
};

export function WebhookOptions({ value, webhookBase, disabled, onChange }: Props) {
  return (
    <fieldset id="webhook-options" disabled={disabled}>
      <legend className="muted">Webhook events</legend>
      {WEBHOOK_EVENTS.map((event) => (
        <Checkbox
          key={event}
          label={event}
          checked={value.includes(event)}
          disabled={disabled}
          onCheckedChange={(checked) =>
            onChange(checked ? [...value, event] : value.filter((item) => item !== event))
          }
        />
      ))}
      <Checkbox label="completed" checked disabled />
      <span className="muted">
        {webhookBase ? `Webhook: ${webhookBase}/webhook/...` : "No webhook host configured"}
      </span>
    </fieldset>
  );
}
