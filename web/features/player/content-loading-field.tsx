"use client";

import {useId, useSyncExternalStore} from "react";
import {useAuth} from "@/features/auth/auth-provider";
import {readContentLoading, subscribeContentLoading, writeContentLoading, type ContentLoadingCapability} from "./content-loading";

export function ContentLoadingField({capability, label = "内容加载"}: {capability?: ContentLoadingCapability | null; label?: string}) {
  const {context} = useAuth();
  const userId = context.user?.userId;
  const id = useId();
  const mode = useSyncExternalStore(subscribeContentLoading, () => readContentLoading(userId), () => "ON_DEMAND");
  if (!capability) {return null;}
  if (capability === "PRELOAD_ONLY") {
    return <div className="field" role="group" aria-label={label}>
      <span>{label}</span>
      <strong>下载完成后开始</strong>
      <small>此运行方式需要完整下载游戏内容。</small>
    </div>;
  }
  return <div className="field">
    <label htmlFor={id}>{label}</label>
    <select id={id} value={mode} onChange={event => writeContentLoading(userId, event.target.value === "PRELOAD" ? "PRELOAD" : "ON_DEMAND")}>
      <option value="ON_DEMAND">按需加载</option>
      <option value="PRELOAD">下载完成后开始</option>
    </select>
    <small>偏好保存在当前设备，已缓存的内容会直接复用。</small>
  </div>;
}
