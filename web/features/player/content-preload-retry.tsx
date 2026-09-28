"use client";

import {useAuth} from "@/features/auth/auth-provider";
import {writeContentLoading} from "./content-loading";

export function ContentPreloadRetry() {
  const {context} = useAuth();
  return <div className="launch-actions">
    <button type="button" className="button" onClick={() => window.location.reload()}>重试下载</button>
    <button type="button" className="button secondary" onClick={() => {
      writeContentLoading(context.user?.userId, "ON_DEMAND");
      window.location.reload();
    }}>改为按需加载</button>
  </div>;
}
