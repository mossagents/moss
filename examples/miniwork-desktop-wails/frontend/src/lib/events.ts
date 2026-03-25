import { useEffect, useRef } from "react";
import { Events } from "@wailsio/runtime";

type Cleanup = () => void;

/**
 * React hook to subscribe to a Wails event. Automatically cleans up on unmount.
 */
export function useWailsEvent<T = any>(
  eventName: string,
  handler: (data: T) => void,
) {
  const handlerRef = useRef(handler);
  handlerRef.current = handler;

  useEffect(() => {
    const off = Events.On(eventName, (ev: any) => {
      handlerRef.current(ev.data);
    });
    return () => { off(); };
  }, [eventName]);
}

/**
 * Subscribe to multiple Wails events at once. Returns a cleanup function.
 */
export function subscribeEvents(
  listeners: Record<string, (data: any) => void>,
): Cleanup {
  const offs: Cleanup[] = [];
  for (const [name, handler] of Object.entries(listeners)) {
    offs.push(Events.On(name, (ev: any) => handler(ev.data)));
  }
  return () => offs.forEach((off) => off());
}
