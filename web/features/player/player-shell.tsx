"use client";
import { useCallback, useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import { api, result } from "@/lib/api/client";
import { useResource } from "@/lib/use-resource";
import { ResourceState } from "@/components/resource-state";
import { useAuth } from "@/features/auth/auth-provider";
import type { Schema } from "@/lib/api/types";
import type {
  PlayerRuntimeV1,
  RuntimeFinalSnapshotV1,
} from "./runtime/contract";
import { validateLaunchEnvelopeBoundary } from "./runtime/envelope";
import { usePlayerSession } from "./use-player-session";
import { usePlayerControls } from "./use-player-controls";
import { useSaveSession } from "./use-save-session";
import { PlayerToolbar } from "./player-toolbar";
import { PlayerSaveChoice } from "./player-save-choice";
import { focusPlayerMenu } from "./player-menu-navigation";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { PlayerFeedback } from "./player-feedback";
import { useReviewCover } from "./use-review-cover";
import type { ContentLoading } from "./content-loading";
import { screenshotFileName } from "./screenshot-file";
import { usePlayerHud } from "./use-player-hud";
export function PlayerShell({
  runId,
  returnTo,
  contentLoading,
}: {
  runId: string;
  returnTo: string;
  contentLoading: ContentLoading;
}) {
  const { context } = useAuth();
  const ownerId = context?.user?.id ?? "";
  const loader = useCallback(async () => {
    if (!ownerId) {
      throw new Error("请登录后运行游戏。");
    }
    const data = result(
      await api.GET("/api/v1/runs/{runId}", { params: { path: { runId } } }),
    );
    validateLaunchEnvelopeBoundary(data.envelope);
    return data;
  }, [runId, ownerId]);
  const run = useResource(loader);
  return (
    <ResourceState resource={run}>
      {(data) => (
        <PlayerContent
          run={data}
          ownerId={ownerId}
          returnTo={returnTo}
          contentLoading={contentLoading}
        />
      )}
    </ResourceState>
  );
}
function PlayerContent({
  run,
  ownerId,
  returnTo,
  contentLoading,
}: {
  run: Schema<"Run">;
  ownerId: string;
  returnTo: string;
  contentLoading: ContentLoading;
}) {
  const router = useRouter();
  const envelope = validateLaunchEnvelopeBoundary(run.envelope);
  const native = envelope.runtime.checkpoint?.semantics === "GAME_SAVE";
  const saves = useSaveSession(run, ownerId, native);
  const reviewCover = useReviewCover(run);
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [exitError, setExitError] = useState("");
  const [confirmExit, setConfirmExit] = useState(false);
  const [menu, setMenu] = useState(false);
  const [saveChoice, setSaveChoice] = useState(false);
  const [restore, setRestore] = useState<RuntimeFinalSnapshotV1 | null>(null);
  const { commit } = saves;
  const requestExit = useCallback(
    (snapshot?: RuntimeFinalSnapshotV1) => {
      if (snapshot) {
        void commit(snapshot, null)
          .catch((failure) =>
            setExitError(
              failure instanceof Error ? failure.message : "无法保留退出存档。",
            ),
          )
          .finally(() => setConfirmExit(true));
      } else {
        setConfirmExit(true);
      }
    },
    [commit],
  );
  const unmountSnapshot = useCallback(
    (snapshot: RuntimeFinalSnapshotV1) => commit(snapshot, null),
    [commit],
  );
  const { mount, runtime, state, error, availability, step } = usePlayerSession(
    run,
    saves.nativeChanged,
    requestExit,
    restore,
    unmountSnapshot,
    ownerId,
    contentLoading,
  );
  const hud = usePlayerHud(
    state,
    confirmExit || menu || saveChoice || settingsOpen,
  );
  async function pause() {
    const instance = readyRuntime(runtime.current);
    if (!instance) {
      return;
    }
    if (instance.getState() === "PAUSED") {
      await instance.resume();
    } else {
      await instance.pause();
    }
  }
  function command(action: Promise<void> | undefined) {
    void action?.catch((failure: unknown) =>
      setExitError(
        failure instanceof Error ? failure.message : "运行操作失败。",
      ),
    );
  }
  async function prepareExit() {
    const instance = runtime.current;
    if (native && instance?.getCheckpointAvailability().available) {
      await saves.capture(instance, "EXPORT");
    }
    setConfirmExit(true);
  }
  async function leave() {
    await runtime.current?.exit();
    if (!saves.draft) {
      await api.DELETE("/api/v1/runs/{runId}", {
        params: { path: { runId: run.id } },
      });
    }
    router.push(
      returnTo.startsWith("/") && !returnTo.startsWith("//")
        ? returnTo
        : "/library",
    );
  }
  usePlayerControls(
    runtime,
    () => {
      hud.show();
      setMenu((value) => !value);
    },
    () => command(pause()),
    confirmExit || menu || saveChoice,
    setExitError,
    menu,
  );
  useEffect(() => {
    if (menu) {
      focusPlayerMenu();
    }
  }, [menu]);
  async function screenshot() {
    const blob = await readyRuntime(runtime.current)?.screenshot();
    if (!blob) {
      return;
    }
    const link = document.createElement("a");
    const url = URL.createObjectURL(blob);
    link.href = url;
    link.download = screenshotFileName(envelope.session.title, blob);
    link.click();
    setTimeout(() => URL.revokeObjectURL(url), 1000);
  }
  async function settings() {
    const instance = readyRuntime(runtime.current);
    if (!instance) {
      return;
    }
    try {
      if (settingsOpen) {
        await instance.closeNativeSettings();
      } else {
        await instance.openNativeSettings("core");
      }
      setSettingsOpen(!settingsOpen);
    } catch (failure) {
      setExitError(
        failure instanceof Error ? failure.message : "无法打开运行设置。",
      );
    }
  }
  useEffect(() => {
    function guard(event: BeforeUnloadEvent) {
      if (saves.draft || saves.busy) {
        event.preventDefault();
        event.returnValue = "";
      }
    }
    window.addEventListener("beforeunload", guard);
    return () => window.removeEventListener("beforeunload", guard);
  }, [saves.draft, saves.busy]);
  return (
    <div className="player-shell">
      <div className="player-stage">
        <div className="player-runtime-mount" ref={mount} />
      </div>
      <PlayerToolbar
        envelope={envelope}
        state={state}
        availability={availability}
        busy={saves.busy || reviewCover.busy}
        status={reviewCover.status || saves.status}
        visible={hud.visible}
        menu={menu}
        onMenu={() => setMenu(!menu)}
        onControlError={setExitError}
        onReveal={hud.show}
        onHover={hud.onHover}
        onFocus={hud.onFocus}
        onPause={() => command(pause())}
        onSave={() => {
          const instance = readyRuntime(runtime.current);
          if (!instance) {
            return;
          }
          if (!native && run.purpose === "play" && saves.currentSave) {
            setSaveChoice(true);
          } else {
            void saves.capture(instance);
          }
        }}
        onExit={() => void prepareExit()}
        onScreenshot={() => command(screenshot())}
        onSettings={() => void settings()}
        settingsOpen={settingsOpen}
        onUseCover={
          run.purpose === "review"
            ? () => void reviewCover.save(readyRuntime(runtime.current))
            : undefined
        }
        onVolume={(value) => command(readyRuntime(runtime.current)?.setVolume(value))}
        onVideo={(value) => command(readyRuntime(runtime.current)?.setVideoMode(value))}
      />
      <PlayerSaveChoice
        open={saveChoice}
        save={saves.currentSave}
        onClose={() => setSaveChoice(false)}
        onSelect={(save) => {
          setSaveChoice(false);
          const instance = readyRuntime(runtime.current);
          if (instance) {
            void saves.capture(instance, "CAPTURE", save);
          }
        }}
      />
      {state === "PAUSED" ? (
        <button
          className="player-pause-overlay is-visible"
          onClick={() => void pause()}
        >
          <span className="player-pause-pill">
            <strong>游戏已暂停</strong>
            <small>点击继续游戏</small>
          </span>
        </button>
      ) : null}
      <PlayerFeedback
        error={error}
        state={state}
        step={step}
        draft={saves.draft}
        busy={saves.busy}
        preview={saves.preview}
        review={run.purpose === "review"}
        onRetry={() => void saves.retry(runtime.current)}
        onRestore={() => setRestore(saves.preview)}
        onReturn={() => setConfirmExit(true)}
      />
      {exitError ? (
        <p className="player-toast is-visible" role="alert">
          {exitError}
        </p>
      ) : null}
      <ConfirmDialog
        open={confirmExit}
        title="退出游戏"
        description={
          saves.draft
            ? "尚有未同步存档。退出后草稿会保留在这个浏览器，可在我的存档中重新同步或导出。"
            : "确认退出当前游戏？"
        }
        confirmLabel="退出游戏"
        onCancel={() => setConfirmExit(false)}
        onConfirm={() => void leave()}
        busy={saves.busy}
      />
    </div>
  );
}

function readyRuntime(instance: PlayerRuntimeV1 | null) {
  return instance && ["RUNNING", "PAUSED"].includes(instance.getState())
    ? instance
    : null;
}
