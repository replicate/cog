import type { ReactNode } from "react";
import { useEffect, useRef } from "react";

/** Concatenates text chunks, recognizes media/URL strings, and follows live output until scrolled. */
export function PredictionOutput({
  metrics,
  running,
  value,
}: {
  metrics?: Record<string, number>;
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
  const viewport = useRef<HTMLDivElement>(null);
  const followTail = useRef(true);
  useEffect(() => {
    if (!running) return;
    followTail.current = true;
    const node = viewport.current;
    if (node) node.scrollTop = node.scrollHeight;
  }, [running]);
  useEffect(() => {
    const node = viewport.current;
    if (node && followTail.current) node.scrollTop = node.scrollHeight;
  }, [children, running]);
  return (
    <section
      ref={viewport}
      className="live-output"
      aria-label="Prediction output"
      aria-busy={running}
      // oxlint-disable-next-line jsx-a11y/no-noninteractive-tabindex -- scrollable output must be keyboard reachable
      tabIndex={0}
      onScroll={(event) => {
        const node = event.currentTarget;
        followTail.current = node.scrollHeight - node.scrollTop - node.clientHeight < 48;
      }}
    >
      {children}
    </section>
  );
}

function Metrics({ metrics }: { metrics: Record<string, number> }) {
  return (
    <table className="metrics-table">
      <caption className="sr-only">Prediction metrics</caption>
      <tbody>
        {Object.entries(metrics).map(([name, value]) => (
          <tr key={name}>
            <th scope="row">{name}</th>
            <td>{String(value)}</td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}

function RenderedOutput({ value, running }: { value: unknown; running: boolean }) {
  if (value === undefined || value === null) {
    return (
      <div className="empty-output">
        {running ? "Waiting for output..." : "Prediction returned no output."}
      </div>
    );
  }
  if (typeof value === "string") return <StringOutput value={value} running={running} />;
  if (Array.isArray(value) && value.every((item) => typeof item === "string")) {
    const strings = value as string[];
    if (strings.some(isMediaValue)) {
      return (
        <div>
          {strings.map((item, index) => (
            <div className="output-item" key={index}>
              {media(item) ?? <TextOutput value={item} />}
            </div>
          ))}
        </div>
      );
    }
    return <TextOutput value={strings.join("")} running={running} />;
  }
  return <TextOutput value={JSON.stringify(value, null, 2)} running={running} />;
}

function StringOutput({ value, running }: { value: string; running: boolean }) {
  const mediaNode = media(value);
  return mediaNode ? (
    <div className="output-item">{mediaNode}</div>
  ) : (
    <TextOutput value={value} running={running} />
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
