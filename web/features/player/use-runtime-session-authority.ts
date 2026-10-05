import {useCallback, type RefObject} from "react";
import type {GameSaveSync} from "./game-save-sync";
import type {PlayProgressClock} from "./play-progress-clock";
import type {PlayerRuntimeV1} from "./runtime/contract";
import type {RuntimeController} from "./runtime/runtime-controller";
import {useRuntimeSessionRenewal} from "./runtime-session-renewal";

type Params = {
  launchId: string;
  uploads: AbortController;
  started: RefObject<boolean>;
  finishing: RefObject<boolean>;
  running: RefObject<boolean>;
  runtime: RefObject<PlayerRuntimeV1 | null>;
  controller: RefObject<RuntimeController | null>;
  nativeSave: RefObject<GameSaveSync | null>;
  progressClock: RefObject<PlayProgressClock>;
  progressTimer: RefObject<number | null>;
  onUnavailable: () => void;
};

export function useRuntimeSessionAuthority({
  launchId, uploads, started: startedRef, finishing: finishingRef, running: runningRef, runtime: runtimeRef,
  controller: controllerRef, nativeSave: nativeSaveRef, progressClock: progressClockRef, progressTimer: progressTimerRef, onUnavailable,
}: Params) {
  const stop = useCallback(() => {
    if (finishingRef.current) {return;}
    finishingRef.current = true;
    startedRef.current = false;
    runningRef.current = false;
    uploads.abort();
    progressClockRef.current.stop(performance.now());
    if (progressTimerRef.current !== null) {window.clearInterval(progressTimerRef.current); progressTimerRef.current = null;}
    // Stop producers before destroying the core; revocation never captures or flushes a save.
    void nativeSaveRef.current?.stop().catch(() => undefined);
    const active = controllerRef.current;
    controllerRef.current = null;
    runtimeRef.current = null;
    void active?.exit().catch(() => undefined);
    onUnavailable();
  }, [uploads, startedRef, finishingRef, runningRef, runtimeRef, controllerRef, nativeSaveRef, progressClockRef, progressTimerRef, onUnavailable]);
  useRuntimeSessionRenewal(launchId, startedRef, finishingRef, stop);
}
