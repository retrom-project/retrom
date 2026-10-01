"use client";

import {useId, useSyncExternalStore} from "react";
import {useAuth} from "@/features/auth/auth-provider";
import {readContentLoading, subscribeContentLoading, writeContentLoading, type ContentLoadingCapability} from "./content-loading";

const subscribeHydration = () => () => undefined;

export function ContentLoadingField({capability, label = "内容加载", hideLabel = false}: {capability?: ContentLoadingCapability | null; label?: string; hideLabel?: boolean}) {
  const {context} = useAuth();
  const userId = context.user?.userId;
  const id = useId();
  const mode = useSyncExternalStore(subscribeContentLoading, () => readContentLoading(userId), () => "ON_DEMAND");
  const hydrated = useSyncExternalStore(subscribeHydration, () => true, () => false);
  if (!capability) {
    return <div className="field content-loading-field is-placeholder" aria-hidden="true" inert>
      <span className={hideLabel ? "sr-only" : "field-label"}>{label}</span>
      <select disabled tabIndex={-1} defaultValue=""><option value="" /></select>
      <small>此运行方式仅支持完整下载游戏内容。</small>
    </div>;
  }
  const preloadOnly = capability === "PRELOAD_ONLY";
  return <div className="field content-loading-field">
    <label htmlFor={id} className={hideLabel ? "sr-only" : undefined}>{label}</label>
    <select id={id} disabled={!hydrated} value={preloadOnly ? "PRELOAD" : mode} onChange={event => {
      if (!preloadOnly) {writeContentLoading(userId, event.target.value === "PRELOAD" ? "PRELOAD" : "ON_DEMAND");}
    }}>
      {!preloadOnly ? <option value="ON_DEMAND">按需加载</option> : null}
      <option value="PRELOAD">下载完成后开始</option>
    </select>
    <small>{preloadOnly ? "此运行方式仅支持完整下载游戏内容。" : "偏好保存在当前设备，已缓存的内容会直接复用。"}</small>
  </div>;
}
