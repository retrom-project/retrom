"use client";

import {useId, useSyncExternalStore} from "react";
import {useAuth} from "@/features/auth/auth-provider";
import {readContentLoading, subscribeContentLoading, writeContentLoading} from "./content-loading";

export function ContentLoadingField() {
  const {context} = useAuth();
  const userId = context.user?.userId;
  const id = useId();
  const mode = useSyncExternalStore(subscribeContentLoading, () => readContentLoading(userId), () => "ON_DEMAND");
  return <div className="field">
    <label htmlFor={id}>内容加载</label>
    <select id={id} value={mode} onChange={event => writeContentLoading(userId, event.target.value === "PRELOAD" ? "PRELOAD" : "ON_DEMAND")}>
      <option value="ON_DEMAND">按需加载</option>
      <option value="PRELOAD">下载完成后开始</option>
    </select>
    <small>适用于支持本地内容缓存的游戏，偏好保存在当前设备。</small>
  </div>;
}
