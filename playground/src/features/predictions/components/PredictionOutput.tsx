import { type ReactNode, useMemo } from "react";

import { useFollowTail } from "@/features/predictions/hooks/useFollowTail";

/** Concatenates text chunks, recognizes media/URL strings, and follows live output until scrolled. */
export function PredictionOutput({
  metrics,
  running,
  value,
}: {
  metrics?: Record<string, unknown>;
  running: boolean;
  value: unknown;
}) {
  return (
    <LiveOutput running={running}>
      {metrics && <Metrics metrics={metrics} />}
      <RenderedOutput value={value} running={running} />
    </LiveOutput>
  );
}

function LiveOutput({ children, running }: { children: ReactNode; running: boolean }) {
  const { ref, onScroll } = useFollowTail<HTMLDivElement>(running, children);
  return (
    <section
      ref={ref}
      className="live-output"
      aria-label="Prediction output"
      aria-busy={running}
      // oxlint-disable-next-line jsx-a11y/no-noninteractive-tabindex -- scrollable output must be keyboard reachable
      tabIndex={0}
      onScroll={onScroll}
    >
      {children}
    </section>
  );
}

function Metrics({ metrics }: { metrics: Record<string, unknown> }) {
  return (
    <table className="metrics-table">
      <caption className="sr-only">Prediction metrics</caption>
      <tbody>
        {Object.entries(metrics).map(([name, value]) => (
          <tr key={name}>
            <th scope="row">{name}</th>
            <td>{formatMetricValue(value)}</td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}

function formatMetricValue(value: unknown): string {
  if (typeof value === "string") return value;
  try {
    return JSON.stringify(value) ?? String(value);
  } catch {
    return String(value);
  }
}

function RenderedOutput({ value, running }: { value: unknown; running: boolean }) {
  // Joining streamed chunks can be multi-MB, so only repeat it when the value changes.
  const rendered = useMemo(
    () => (isStringArray(value) && !value.some(isMediaValue) ? value.join("") : value),
    [value],
  );
  if (rendered === undefined || rendered === null) {
    return (
      <div className="empty-output">
        {running ? "Waiting for output..." : "Prediction returned no output."}
      </div>
    );
  }
  if (isStringArray(rendered)) {
    return (
      <div>
        {rendered.map((item, index) => (
          <div className="output-item" key={index}>
            {media(item) ?? <TextOutput value={item} />}
          </div>
        ))}
      </div>
    );
  }
  if (typeof rendered === "string") {
    const mediaNode = media(rendered);
    return mediaNode ? (
      <div className="output-item">{mediaNode}</div>
    ) : (
      <TextOutput value={rendered} running={running} />
    );
  }
  return (
    <TextOutput value={JSON.stringify(rendered, null, 2) ?? String(rendered)} running={running} />
  );
}

function TextOutput({ value, running = false }: { value: string; running?: boolean }) {
  return (
    <pre className="text-output">
      {value}
      {running && <span className="streaming-cursor" />}
    </pre>
  );
}

function isMediaValue(value: string): boolean {
  return /^data:[^,]*,/i.test(value) || /^https?:\/\//i.test(value);
}

function isStringArray(value: unknown): value is string[] {
  return Array.isArray(value) && value.every((item) => typeof item === "string");
}

function media(value: string): ReactNode {
  if (!isMediaValue(value)) return null;
  if (/^data:image\//i.test(value)) return <img src={value} alt="Prediction output" />;
  if (/^data:audio\//i.test(value)) {
    // oxlint-disable-next-line jsx-a11y/media-has-caption -- model outputs do not include caption tracks
    return <audio controls src={value} />;
  }
  if (/^data:video\//i.test(value)) {
    // oxlint-disable-next-line jsx-a11y/media-has-caption -- model outputs do not include caption tracks
    return <video controls src={value} />;
  }
  if (/^data:/i.test(value)) {
    return (
      <a href={value} download="output">
        Download file
      </a>
    );
  }
  return (
    <a href={value} target="_blank" rel="noopener noreferrer">
      {value}
    </a>
  );
}
