import { Checkbox } from "@cloudflare/kumo/components/checkbox";

import { WEBHOOK_EVENTS, type WebhookEvent } from "../../domain/types";

type Props = {
  value: WebhookEvent[];
  webhookBase: string;
  onChange: (value: WebhookEvent[]) => void;
};

export function WebhookOptions({ value, webhookBase, onChange }: Props) {
  return (
    <fieldset id="webhook-options">
      <legend className="muted">Webhook events</legend>
      {WEBHOOK_EVENTS.map((event) => (
        <Checkbox
          key={event}
          label={event}
          checked={value.includes(event)}
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
