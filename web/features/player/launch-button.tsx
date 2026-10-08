"use client";
import { useState } from "react";
import { useRouter } from "next/navigation";
import { useToast } from "@/components/toast-provider";
import { api, result } from "@/lib/api/client";
import type { ContentLoading } from "./content-loading";
export function LaunchButton({
  gameId,
  coreId,
  saveId,
  disabled = false,
  variant = "primary",
  purpose = "play",
  children = "开始游戏",
  returnTo,
  contentLoading,
}: {
  gameId: string;
  coreId?: string;
  saveId?: string;
  disabled?: boolean;
  variant?: "primary" | "secondary";
  purpose?: "play" | "review";
  children?: React.ReactNode;
  returnTo?: string;
  contentLoading?: ContentLoading;
}) {
  const [busy, setBusy] = useState(false);
  const { notify, clear } = useToast();
  const router = useRouter();
  async function launch() {
    setBusy(true);
    clear();
    try {
      const run = result(
        await api.POST("/api/v1/runs", {
          body: { gameId, coreId, saveId, purpose },
        }),
      );
      router.replace(
        `/play/${run.id}?returnTo=${encodeURIComponent(returnTo ?? `${location.pathname}${location.search}${location.hash}`)}${contentLoading ? `&contentLoading=${contentLoading}` : ""}`,
      );
    } catch (failure) {
      notify({ tone: "bad", message: failure instanceof Error ? failure.message : "启动失败。" });
      setBusy(false);
    }
  }
  return (
    <div className="home-launch-control">
      <button
        className={`button${variant === "secondary" ? " secondary" : ""}`}
        disabled={disabled || busy}
        onClick={() => void launch()}
      >
        {busy ? "正在启动…" : children}
      </button>

    </div>
  );
}
