import { Tabs } from "@cloudflare/kumo/components/tabs";

type TabItem = {
  value: string;
  label: string;
};

type Props = {
  label: string;
  items: TabItem[];
  value: string;
  onChange: (value: string) => void;
};

export function SegmentedTabs({ label, items, value, onChange }: Props) {
  return (
    <div aria-label={label} className="playground-segmented">
      <Tabs tabs={items} value={value} onValueChange={onChange} variant="segmented" size="sm" />
    </div>
  );
}
