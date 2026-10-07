"use client";
import { useCallback, useEffect, useRef, useState } from "react";
import { useRouter } from "next/navigation";
import { api, result, ApiError } from "@/lib/api/client";
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
import { useRuntimeShortcuts } from "./use-runtime-shortcuts";
import { useSaveSession } from "./use-save-session";
import { PlayerToolbar } from "./player-toolbar";
import { PlayerSaveChoice } from "./player-save-choice";
import { focusPlayerMenu } from "./player-menu-navigation";
import { PlayerExitDialog } from "./player-exit-dialog";
import { usePlayerExit } from "./use-player-exit";
import { useToast } from "@/components/toast-provider";
import { PlayerFeedback } from "./player-feedback";
import { useReviewCover } from "./use-review-cover";
import type { ContentLoading } from "./content-loading";
import { usePlayerVolume } from "./use-player-volume";
import { usePlayerHud } from "./use-player-hud";
import { usePlayerMenuDismiss } from "./use-player-menu-dismiss";
import { usePlayerEditor } from "./use-player-editor";
import { GameEditorPanel } from "./game-editor-panel";
import { usePlayerSettings } from "./use-player-settings";
import { EmulatorSettingsLayer } from "./player-settings";
import { useMobilePlayerLayout, usePlayerDebugState } from "./player-layout";
import { PlayerDebugPanel } from "./player-debug-panel";
import { AppIcon } from "@/components/app-icon";
import { markImmersivePlayerReturn } from "@/features/immersive/active-gamepad";
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
  const mobile = useMobilePlayerLayout();
  const [debugOpen, setDebugOpen] = usePlayerDebugState();
  const unmutedVolume = useRef(1);
  const { notify } = useToast();
  const reportError = useCallback((message: string) => notify({ tone: "bad", message }), [notify]);
  const immersive = returnTo.startsWith("/immersive");
  const exitRequest = useRef<() => void>(() => {});
  const [menu, setMenu] = useState(false);
  const [saveChoice, setSaveChoice] = useState(false);
  const [restore, setRestore] = useState<RuntimeFinalSnapshotV1 | null>(null);
  const { commit } = saves;
  const requestExit = useCallback(
    (snapshot?: RuntimeFinalSnapshotV1) => {
      if (snapshot) {
        void commit(snapshot, null)
          .catch((failure) =>
            reportError(
              failure instanceof Error ? failure.message : "无法保留退出存档。",
            ),
          )
          .finally(() => exitRequest.current());
      } else {
        exitRequest.current();
      }
    },
    [commit, reportError],
  );
  const unmountSnapshot = useCallback(
    async (snapshot: RuntimeFinalSnapshotV1) => {
      if (!await commit(snapshot, null)) { throw new Error("退出存档尚未同步，草稿已保留，请在我的存档中重试或导出。"); }
    },
    [commit],
  );
  const { mount, runtime, state, error, availability, step, tasks } = usePlayerSession(
    run,
    saves.nativeChanged,
    requestExit,
    restore,
    unmountSnapshot,
    ownerId,
    contentLoading,
    saves.settle,
  );
  const volume = usePlayerVolume(runtime, state, returnTo.startsWith("/immersive"), reportError);
  const settings = usePlayerSettings(runtime, state, reportError);
  const editor = usePlayerEditor(runtime, state, reportError);

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
      reportError(
        failure instanceof Error ? failure.message : "运行操作失败。",
      ),
    );
  }
  async function leave() {
    await runtime.current?.exit();
    if (!immersive && document.fullscreenElement) { await document.exitFullscreen(); }
    if (!saves.draft) {
      const response = await api.DELETE("/api/v1/runs/{runId}", { params: { path: { runId: run.id } } });
      if (response.error) { throw new ApiError(response.error.code, response.error.message, response.response.status); }
    }
    if (immersive) { markImmersivePlayerReturn(); }
    router.push(
      returnTo.startsWith("/") && !returnTo.startsWith("//")
        ? returnTo
        : "/library",
    );
  }
  const exit = usePlayerExit({ runtime, immersive, native, hasDraft: !!saves.draft, saving: saves.busy, retry: saves.retry, capture: saves.capture, leave, reportError });
  useEffect(() => { exitRequest.current = () => void exit.request(); }, [exit]);
  const overlays = playerOverlayState({ exit, menu, saveChoice, settings, debugOpen, editor });
  const hud = usePlayerHud(state, overlays.pinned);
  usePlayerMenuDismiss(mount, menu, () => setMenu(false));
  function requestExitMenu() { setMenu(false); void exit.request(); }
  function showMenu() { hud.show(); if (immersive) { requestExitMenu(); } else { setMenu((value) => !value); } }
  const inputSuppressed = overlays.suppressInput;
  useRuntimeShortcuts({ runtime, state, immersive, suppressed: inputSuppressed, onMenu: showMenu, onPause: () => command(pause()), onError: reportError });
  usePlayerControls(runtime, showMenu, () => command(pause()), {
    suppressInput: inputSuppressed,
    menuOpen: immersive ? exit.open : menu,
    immersive,
    dialogOpen: overlays.dialogOpen,
    onCancel: () => { if (editor.open) { editor.close(); } else if (exit.open) { void exit.cancel(); } else { setSaveChoice(false); } },
    onFailure: reportError,
  });
  useEffect(() => {
    if (menu) {
      focusPlayerMenu();
    }
  }, [menu]);
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
    <div className={`player-shell${immersive ? " is-immersive" : ""}`}>
      <div className="player-stage">
        <div className="player-runtime-mount" ref={mount} />
      </div>
      <PlayerToolbar immersive={immersive}
        envelope={envelope}
        state={state}
        availability={availability}
        busy={saves.busy || reviewCover.busy}
        status={saves.status}
        visible={hud.visible}
        menu={!immersive && menu}
        onMenu={() => immersive ? requestExitMenu() : setMenu(!menu)}
        onControlError={reportError}
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
            void saves.capture(instance, native ? "EXPORT" : "CAPTURE");
          }
        }}
        onExit={requestExitMenu}
        onSettings={() => { setMenu(false); void settings.show(); }}
        onGameEditor={editor.editor ? () => { setMenu(false); void editor.show(); } : undefined}
        debugOpen={debugOpen}
        onDebug={() => setDebugOpen(!debugOpen)}
        onUseCover={
          run.purpose === "review"
            ? () => void reviewCover.save(readyRuntime(runtime.current))
            : undefined
        }
      />
      <PlayerDebugPanel open={debugOpen} runtime={runtime} envelope={envelope} state={state} onClose={() => setDebugOpen(false)} />
      <EmulatorSettingsLayer open={settings.open} mobile={mobile} capabilities={envelope.runtime.capabilities} volume={volume.volume} muted={volume.volume === 0} renderingMode={settings.mode} onHold={hud.show}
        onVolume={volume.change} onMute={() => { if (volume.volume) { unmutedVolume.current = volume.volume; volume.change(0); } else { volume.change(unmutedVolume.current); } }}
        onRenderingMode={(value) => void settings.changeVideo(value)} onOpenPanel={settings.navigate} onClose={() => settings.navigate("close")} />
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
      <PlayerPauseOverlay state={state} immersive={immersive} settingsOpen={settings.open} onResume={() => { setMenu(false); command(pause()); }} />
      <PlayerFeedback
        error={error}
        state={state}
        step={step}
        tasks={tasks}
        draft={saves.draft}
        status={saves.status}
        busy={saves.busy}
        preview={saves.preview}
        review={run.purpose === "review"}
        onRetry={() => void saves.retry(runtime.current)}
        onRestore={() => setRestore(saves.preview)}
        onReturn={requestExitMenu}
      />

      {!editor.open ? <PlayerExitDialog immersive={immersive} native={native} available={availability.available} draft={!!saves.draft} controller={exit} onGameEditor={editor.editor ? () => void editor.show() : undefined} /> : null}
      {editor.open && editor.editor ? <GameEditorPanel editor={editor.editor} immersive={immersive} native={native} onClose={editor.close} /> : null}
    </div>
  );
}

function readyRuntime(instance: PlayerRuntimeV1 | null) {
  return instance && ["RUNNING", "PAUSED"].includes(instance.getState())
    ? instance
    : null;
}

function PlayerPauseOverlay({ state, immersive, settingsOpen, onResume }: { state: string; immersive: boolean; settingsOpen: boolean; onResume: () => void }) {
  if (state !== "PAUSED" || immersive) { return null; }
  return <button className={`player-pause-overlay is-visible${settingsOpen ? " is-settings-passthrough" : ""}`} onClick={onResume}><span className="player-pause-pill"><AppIcon name="pause" /><strong>已暂停</strong><small>点击游戏画面继续</small></span></button>;
}

function playerOverlayState({ exit, menu, saveChoice, settings, debugOpen, editor }: {
  exit: { open: boolean; active: boolean }; menu: boolean; saveChoice: boolean;
  settings: { open: boolean }; debugOpen: boolean; editor: { open: boolean; pending: boolean };
}) {
  const dialogOpen = exit.active || saveChoice || editor.open;
  return {
    dialogOpen,
    suppressInput: dialogOpen || menu || settings.open || editor.pending,
    pinned: exit.open || menu || saveChoice || settings.open || debugOpen || editor.open,
  };
}
