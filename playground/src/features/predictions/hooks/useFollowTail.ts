import { useCallback, useEffect, useRef } from "react";

type FollowTail<T extends HTMLElement> = {
  ref: React.RefObject<T | null>;
  onScroll: (event: React.UIEvent<T>) => void;
};

const TAIL_THRESHOLD_PX = 48;

/**
 * Keeps a visible scroll container pinned to the bottom while content grows, releasing the pin if
 * the user scrolls up so a populating response can be read without fighting the view.
 */
export function useFollowTail<T extends HTMLElement>(
  follow: boolean,
  dependency: unknown,
  active = true,
): FollowTail<T> {
  const ref = useRef<T>(null);
  const followTail = useRef(true);

  useEffect(() => {
    if (!follow) return;
    followTail.current = true;
  }, [follow]);

  useEffect(() => {
    if (!follow || !active) return;
    const node = ref.current;
    if (node && followTail.current) node.scrollTop = node.scrollHeight;
  }, [dependency, active, follow]);

  const onScroll = useCallback((event: React.UIEvent<T>) => {
    const node = event.currentTarget;
    followTail.current = node.scrollHeight - node.scrollTop - node.clientHeight < TAIL_THRESHOLD_PX;
  }, []);

  return { ref, onScroll };
}
