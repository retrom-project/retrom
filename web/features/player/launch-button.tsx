"use client";
import { useContext, useRef, useState } from "react";
import { useRouter } from "next/navigation";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { AppIcon } from "@/components/app-icon";
import type { Save } from "@/lib/api/types";
import { RestoreConfirmationContext, restoreConfirmationTitle, restoreConfirmationDescription, useRestoreConfirmation } from "./restore-confirmation";
import { useToast } from "@/components/toast-provider";
import { api, result } from "@/lib/api/client";
import type { ContentLoading } from "./content-loading";
import { cancelLaunchFullscreen, requestPlayerFullscreen } from "./player-fullscreen";
export function LaunchButton({
  gameId,
  coreId,
  saveId,
  restoreWarning,
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
  restoreWarning?: Save["restoreWarning"];
  disabled?: boolean;
  variant?: "primary" | "secondary";
  purpose?: "play" | "review";
  children?: React.ReactNode;
  returnTo?: string;
  contentLoading?: ContentLoading;
}) {
  const [busy, setBusy] = useState(false);
  const button = useRef<HTMLButtonElement>(null);
  const { notify, clear } = useToast();
  const router = useRouter();
  const confirmation = useRestoreConfirmation();
  const immersiveConfirmation = useContext(RestoreConfirmationContext);
  const confirmRestore = immersiveConfirmation ?? confirmation.request;
  async function launch() {
    if (busy || confirmation.open) { return; }
    const confirmed = restoreWarning === "core_changed";
    if (confirmed && !await confirmRestore()) { return; }
    setBusy(true);
    clear();
    const fullscreen = requestPlayerFullscreen();
    try {
      const run = result(
        await api.POST("/api/v1/runs", {
          body: { gameId, coreId, saveId, purpose },
        }),
      );
      await fullscreen;
      // The installed core may have changed since this page fetched its saves.
      if (run.save?.restoreWarning === "core_changed" && !confirmed) {
        await cancelLaunchFullscreen(await fullscreen);
        if (!await confirmRestore()) {
          const removed = await api.DELETE("/api/v1/runs/{runId}", { params: { path: { runId: run.id } } });
          if (removed.error) { throw new Error(removed.error.message); }
          setBusy(false);
          requestAnimationFrame(() => button.current?.focus());
          return;
        }
        await requestPlayerFullscreen();
      }
      router.replace(
        `/play/${run.id}?returnTo=${encodeURIComponent(returnTo ?? `${location.pathname}${location.search}${location.hash}`)}${contentLoading ? `&contentLoading=${contentLoading}` : ""}`,
      );
    } catch (failure) {
      await cancelLaunchFullscreen(await fullscreen);
      notify({ tone: "bad", message: failure instanceof Error ? failure.message : "启动失败。" });
      setBusy(false);
    }
  }
  return (
    <div className="home-launch-control">
      <button
        ref={button}
        className={`button${variant === "secondary" ? " secondary" : ""}`}
        title={restoreWarning === "core_changed" ? "核心版本已变化，加载可能失败" : undefined}
        aria-description={restoreWarning === "core_changed" ? "核心版本已变化，确认后可尝试加载" : undefined}
        disabled={disabled || busy}
        onClick={() => void launch()}
      >
        {restoreWarning === "core_changed" ? <AppIcon name="warning" /> : null}
        {busy ? "正在启动…" : children}
      </button>
      <ConfirmDialog open={confirmation.open} title={restoreConfirmationTitle} description={restoreConfirmationDescription}
        confirmLabel="尝试加载" onConfirm={() => confirmation.settle(true)} onCancel={() => confirmation.settle(false)} portalToBody />
    </div>
  );
}
