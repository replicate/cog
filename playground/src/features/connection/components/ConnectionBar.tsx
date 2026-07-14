import { Button } from "@cloudflare/kumo/components/button";
import { Input } from "@cloudflare/kumo/components/input";

type Props = {
  draft: string;
  status?: string;
  disabled: boolean;
  onDraftChange: (value: string) => void;
  onConnect: () => void;
};

/** Submits the target on Enter as well as button activation without owning connection state. */
export function ConnectionBar({ draft, status, disabled, onDraftChange, onConnect }: Props) {
  return (
    <fieldset id="target-bar">
      <legend className="sr-only">Target connection</legend>
      <div className="target-input">
        <Input
          label="Target"
          type="url"
          value={draft}
          disabled={disabled}
          onInput={(event) => onDraftChange(event.currentTarget.value)}
          onKeyDown={(event) => {
            if (event.key === "Enter") onConnect();
          }}
        />
      </div>
      <Button variant="primary" disabled={disabled || !draft.trim()} onClick={onConnect}>
        Connect
      </Button>
      <output className="muted">{status}</output>
    </fieldset>
  );
}
