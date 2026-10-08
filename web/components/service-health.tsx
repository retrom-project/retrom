"use client";

import { useEffect, useRef, useState } from "react";
import { readError } from "@/lib/api/client";
import { ResponsiveSheet } from "./responsive-sheet";

type Health = {
  state: "checking" | "ready" | "unavailable";
  detail: string;
};

async function checkHealth(signal: AbortSignal): Promise<Health> {
  const response = await fetch("/health/ready", { cache: "no-store", signal });
  if (response.ok) {
    return { state: "ready", detail: "服务已通过就绪检查。" };
  }
  const error = readError(await response.json().catch(() => null));
  const message = error.code === "SERVICE_UNAVAILABLE"
    ? "服务暂时无法完成就绪检查，请稍后再试。"
    : error.code === "INTERNAL_ERROR"
      ? "就绪检查未能完成，请稍后再试。"
      : error.message;
  return {
    state: "unavailable",
    detail: `${message}（HTTP ${response.status}）`,
  };
}

export function ServiceHealth({ compact = false }: { compact?: boolean }) {
  const [open, setOpen] = useState(false);
  const [health, setHealth] = useState<Health>({
    state: "checking", detail: "正在检查服务是否就绪。",
  });
  const buttonRef = useRef<HTMLButtonElement>(null);
  useEffect(() => {
    const controller = new AbortController();
    void checkHealth(controller.signal).then(
      (next) => {
        if (!controller.signal.aborted) {
          setHealth(next);
        }
      },
      () => {
        if (!controller.signal.aborted) {
          setHealth({ state: "unavailable", detail: "服务连接失败，请检查网络后刷新页面。" });
        }
      },
    );
    return () => controller.abort();
  }, []);
  const label = health.state === "checking"
    ? "正在检查服务" : health.state === "ready" ? "服务正常" : "服务存在异常";

  if (!compact) {
    return <span className={`connection ${health.state}`} aria-live="polite" tabIndex={0}>
      <i aria-hidden="true" />
      <span className="connection-tooltip" role="tooltip">
        <strong>{label}</strong><small>{health.detail}</small>
      </span>
    </span>;
  }
  return <>
    <button
      ref={buttonRef}
      type="button"
      className={`connection compact-health ${health.state}`}
      aria-label={`服务器状态：${label}`}
      aria-haspopup="dialog"
      aria-expanded={open}
      onClick={() => setOpen(true)}
    ><i aria-hidden="true" /></button>
    <ResponsiveSheet
      open={open}
      title="服务状态"
      description="当前 Retrom 后端就绪检查。"
      placement="bottom"
      returnFocusRef={buttonRef}
      className="compact-info-sheet"
      onClose={() => setOpen(false)}
    >
      <div className={`compact-health-detail is-${health.state}`} role="status">
        <i aria-hidden="true" />
        <div><strong>{label}</strong><p>{health.detail}</p></div>
      </div>
    </ResponsiveSheet>
  </>;
}
