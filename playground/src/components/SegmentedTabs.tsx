import { Tabs } from "@cloudflare/kumo/components/tabs";
import { useEffect, useRef } from "react";

type TabItem<Value extends string> = {
  value: Value;
  label: string;
};

type Props<Value extends string> = {
  id: string;
  label: string;
  items: readonly TabItem<Value>[];
  value: Value;
  onChange: (value: Value) => void;
};

/** Generates the ID referenced by a matching panel's `aria-labelledby`. */
export function tabId(id: string, value: string): string {
  return `${id}-tab-${value}`;
}

/** Generates the panel ID referenced by a matching tab's `aria-controls`. */
export function tabPanelId(id: string, value: string): string {
  return `${id}-panel-${value}`;
}

/** Adds stable ARIA tab/panel relationships around Kumo's controlled tabs. */
export function SegmentedTabs<Value extends string>({
  id,
  label,
  items,
  value,
  onChange,
}: Props<Value>) {
  const root = useRef<HTMLDivElement>(null);
  useEffect(() => {
    root.current?.querySelector('[role="tablist"]')?.setAttribute("aria-label", label);
  }, [label]);

  return (
    <div ref={root} className="playground-segmented">
      <Tabs
        tabs={items.map((item) => ({
          ...item,
          render: (
            <button
              id={tabId(id, item.value)}
              type="button"
              aria-label={item.label}
              aria-controls={tabPanelId(id, item.value)}
            />
          ),
        }))}
        value={value}
        activateOnFocus
        onValueChange={(next) => {
          const item = items.find((candidate) => candidate.value === next);
          if (item) onChange(item.value);
        }}
        variant="segmented"
        size="sm"
      />
    </div>
  );
}
