import { Checkbox } from "@cloudflare/kumo/components/checkbox";

type Props = {
  value: string[];
  webhookBase: string;
  onChange: (value: string[]) => void;
};

export function WebhookOptions({ value, webhookBase, onChange }: Props) {
  return (
    <div id="webhook-options">
      <span className="muted">Webhook events</span>
      {["start", "output", "logs"].map((event) => (
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
    </div>
  );
}
