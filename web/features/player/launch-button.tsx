"use client";
import { useState } from "react";
import { useRouter } from "next/navigation";
import { api, result } from "@/lib/api/client";
import type { ContentLoading } from "./content-loading";
export function LaunchButton({
  gameId,
  coreId,
  saveId,
  disabled = false,
  purpose = "play",
  children = "开始游戏",
  returnTo,
  contentLoading,
}: {
  gameId: string;
  coreId?: string;
  saveId?: string;
  disabled?: boolean;
  purpose?: "play" | "review";
  children?: React.ReactNode;
  returnTo?: string;
  contentLoading?: ContentLoading;
}) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const router = useRouter();
  async function launch() {
    setBusy(true);
    setError("");
    try {
      const run = result(
        await api.POST("/api/v1/runs", {
          body: { gameId, coreId, saveId, purpose },
        }),
      );
      router.push(
        `/play/${run.id}?returnTo=${encodeURIComponent(returnTo ?? location.pathname)}${contentLoading ? `&contentLoading=${contentLoading}` : ""}`,
      );
    } catch (failure) {
      setError(failure instanceof Error ? failure.message : "启动失败。");
      setBusy(false);
    }
  }
  return (
    <div className="home-launch-control">
      <button
        className="button"
        disabled={disabled || busy}
        onClick={() => void launch()}
      >
        {busy ? "正在启动…" : children}
      </button>
      {error ? (
        <span className="restore-reason" role="alert">
          {error}
        </span>
      ) : null}
    </div>
  );
}
