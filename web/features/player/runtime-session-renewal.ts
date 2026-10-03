import {useEffect, type RefObject} from "react";

// Authority checks share the existing renewal route and stay independent of play statistics.
export function useRuntimeSessionRenewal(
  launchId: string, started: RefObject<boolean>, finishing: RefObject<boolean>, onUnavailable: () => void,
) {
  useEffect(() => {
    let pending = false;
    let unavailable = false;
    const controller = new AbortController();
    const renew = async () => {
      if (pending || unavailable || !started.current || finishing.current || controller.signal.aborted) {return;}
      pending = true;
      try {
        const response = await fetch(`/runtime/launches/${launchId}/renew`, {
          method: "POST", credentials: "same-origin", cache: "no-store",
          signal: AbortSignal.any([controller.signal, AbortSignal.timeout(5_000)]),
        });
        if (response.status === 401 && !controller.signal.aborted && !finishing.current) {
          unavailable = true;
          onUnavailable();
        }
        await response.body?.cancel();
      } catch {
        // A temporary outage must not stop the core; the next tick retries.
      } finally {
        pending = false;
      }
    };
    const onVisible = () => {if (document.visibilityState === "visible") {void renew();}};
    const timer = window.setInterval(() => {void renew();}, 15_000);
    document.addEventListener("visibilitychange", onVisible);
    window.addEventListener("online", onVisible);
    return () => {
      controller.abort();
      window.clearInterval(timer);
      document.removeEventListener("visibilitychange", onVisible);
      window.removeEventListener("online", onVisible);
    };
  }, [launchId, started, finishing, onUnavailable]);
}
