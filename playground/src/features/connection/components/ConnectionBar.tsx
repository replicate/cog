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
    <div id="target-bar">
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
      <Button disabled={disabled || !draft.trim()} onClick={onConnect}>
        Connect
      </Button>
      <output className="muted">{status}</output>
    </div>
  );
}
