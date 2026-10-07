import type { ReactNode } from "react";
import type { Resource } from "@/lib/use-resource";
import { FeedbackBanner } from "./ui";
export function ResourceState<T>({
  resource,
  children,
}: {
  resource: Resource<T>;
  children: (data: T) => ReactNode;
}) {
  if (resource.loading) {
    return (
      <div className="empty" role="status">
        <span className="button-spinner" />
        正在加载…
      </div>
    );
  }
  if (resource.error) {
    return (
      <FeedbackBanner tone="bad">
        {resource.error}
        <button className="button secondary" onClick={resource.reload}>
          重试
        </button>
      </FeedbackBanner>
    );
  }
  return resource.data ? <>{children(resource.data)}</> : null;
}
