import { useEffect } from "react";
import type { ReviewWorkspace } from "./review-actions-model";

export function useReviewCurrentFacts(itemId: string, apply: (review: ReviewWorkspace) => void) {
  useEffect(() => {
    let pending = false;
    const controller = new AbortController();
    async function refresh() {
      if (pending || document.visibilityState === "hidden") { return; }
      pending = true;
      try {
        const response = await fetch(`/api/v1/admin/reviews/${itemId}`, { cache: "no-store", signal: controller.signal });
        if (response.ok && !controller.signal.aborted) { apply(await response.json() as ReviewWorkspace); }
      } catch (error) {
        if (!controller.signal.aborted) { console.error("Unable to refresh review facts", error); }
      } finally { pending = false; }
    }
    const onVisible = () => { void refresh(); };
    window.addEventListener("focus", onVisible);
    document.addEventListener("visibilitychange", onVisible);
    return () => {
      controller.abort();
      window.removeEventListener("focus", onVisible);
      document.removeEventListener("visibilitychange", onVisible);
    };
  }, [itemId, apply]);
}
