import {useEffect, type RefObject} from "react";

// This is independent of play statistics and never gates game execution.
export function useRuntimeSessionRenewal(launchId: string, started: RefObject<boolean>, finishing: RefObject<boolean>) {
  useEffect(() => {
    let pending = false;
    const controller = new AbortController();
    const renew = async () => {
      if (pending || !started.current || finishing.current || controller.signal.aborted) {return;}
      pending = true;
      try {
        await fetch(`/runtime/launches/${launchId}/renew`, {
          method: "POST", credentials: "same-origin", cache: "no-store", signal: controller.signal,
        });
      } catch {
        // A temporary outage must not stop the core; the next tick retries.
      } finally {
        pending = false;
      }
    };
    const onVisible = () => {if (document.visibilityState === "visible") {void renew();}};
    const timer = window.setInterval(() => {void renew();}, 60 * 60 * 1000);
    document.addEventListener("visibilitychange", onVisible);
    window.addEventListener("online", onVisible);
    return () => {
      controller.abort();
      window.clearInterval(timer);
      document.removeEventListener("visibilitychange", onVisible);
      window.removeEventListener("online", onVisible);
    };
  }, [launchId, started, finishing]);
}
