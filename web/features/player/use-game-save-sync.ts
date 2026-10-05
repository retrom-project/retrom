"use client";

import {useEffect, useRef} from "react";
import {holdDraftLease} from "./game-save-draft-lease";
import {gameSaveDraftStore} from "./game-save-draft-store";
import {GameSaveSync, type GameSavePresentation} from "./game-save-sync";
import type {LaunchEnvelopeV1, PlayerRuntimeV1} from "./runtime/contract";
import type {RuntimeSavePayload} from "./runtime/runtime-actions";

export function useGameSaveSync(
  enabled: boolean,
  runtime: {current: PlayerRuntimeV1 | null},
  upload: (payload: RuntimeSavePayload) => Promise<boolean>,
  present: (state: GameSavePresentation) => void,
  userId: string | undefined,
  envelope: {current: LaunchEnvelopeV1 | null},
) {
  const sync = useRef<GameSaveSync | null>(null);
  useEffect(() => {
    const launch = envelope.current;
    if (!enabled || !runtime.current || !userId || !launch) {return;}
    const store = launch.session.purpose === "PRODUCT" ? gameSaveDraftStore({
      userId, launchId: launch.session.id, title: launch.session.title, restored: launch.restore !== null,
    }) : {put: async () => undefined, remove: async () => undefined};
    const active = new GameSaveSync(runtime.current, upload, present, store);
    const release = holdDraftLease(userId, launch.session.id);
    sync.current = active;
    active.start();
    return () => {
      void active.stop().finally(release);
      if (sync.current === active) {sync.current = null;}
    };
  }, [enabled, runtime, upload, present, userId, envelope]);
  return sync;
}
