"use client";
import { useEffect, useRef, useState } from "react";
import type { Schema } from "@/lib/api/types";
import { api } from "@/lib/api/client";
import type {
  LaunchEnvelopeV1,
  PlayerRuntimeV1,
  RuntimeCheckpointAvailabilityV1,
  RuntimeEventV1,
  RuntimeFinalSnapshotV1,
  RuntimeStateV1,
} from "./runtime/contract";
import { disposeRuntime } from "./runtime/dispose-runtime";
import { startupLabel } from "./startup-label";
import { createRuntimeHost } from "./runtime/runtime-host";
import { loadProviderRuntime } from "./runtime/provider-dispatcher";
import { validateLaunchEnvelopeBoundary } from "./runtime/envelope";
import type { ContentLoading } from "./content-loading";
export function usePlayerSession(
  run: Schema<"Run">,
  onNativeChange: (runtime: PlayerRuntimeV1) => void,
  onExit: (snapshot?: RuntimeFinalSnapshotV1) => void,
  restore: RuntimeFinalSnapshotV1 | null,
  onUnmountSnapshot: (snapshot: RuntimeFinalSnapshotV1) => Promise<void>,
  userId: string,
  contentLoading: ContentLoading,
) {
  const mount = useRef<HTMLDivElement>(null);
  const runtime = useRef<PlayerRuntimeV1 | null>(null);
  const callbacks = useRef({ onNativeChange, onExit, onUnmountSnapshot });
  useEffect(() => {
    callbacks.current = { onNativeChange, onExit, onUnmountSnapshot };
  }, [onNativeChange, onExit, onUnmountSnapshot]);
  const [state, setState] = useState<RuntimeStateV1>("CREATED");
  const [error, setError] = useState("");
  const [availability, setAvailability] =
    useState<RuntimeCheckpointAvailabilityV1>({
      available: false,
      reason: null,
    });
  const [step, setStep] = useState("正在准备运行环境…");
  useEffect(() => {
    const controller = new AbortController();
    let unsubscribe: (() => void) | undefined;
    let instance: PlayerRuntimeV1 | null = null;
    function receive(event: RuntimeEventV1) {
      if (controller.signal.aborted) {
        return;
      }
      if (event.type === "STATE_CHANGED") {
        setState(event.state);
      }
      if (event.type === "LOAD_TASK") {
        setStep(startupLabel(event.task.kind));
      }
      if (event.type === "FATAL_ERROR") {
        setError(
          event.failure.diagnostics.map((item) => item.message).join("\n") ||
            event.failure.code,
        );
      }
      if (event.type === "EXIT_REQUESTED") {
        callbacks.current.onExit(event.finalSnapshot);
      }
      if (event.type === "CHECKPOINT_AVAILABILITY_CHANGED") {
        setAvailability(event.availability);
        if (
          instance &&
          event.availability.available &&
          event.availability.revision
        ) {
          callbacks.current.onNativeChange(instance);
        }
      }
    }
    async function start() {
      try {
        const original = validateLaunchEnvelopeBoundary(run.envelope);
        const envelope: LaunchEnvelopeV1 = restore
          ? {
              ...original,
              restore: {
                kind: "LOCAL",
                format: restore.checkpoint.format,
                sha256: await hash(restore.checkpoint.bytes),
                sizeBytes: restore.checkpoint.bytes.length,
              },
            }
          : original;
        instance = await loadProviderRuntime(
          envelope,
          createRuntimeHost(
            envelope,
            controller.signal,
            restore?.checkpoint.bytes ?? null,
            contentLoading,
          ),
        );
        if (controller.signal.aborted) {
          await instance.exit();
          return;
        }
        runtime.current = instance;
        unsubscribe = instance.subscribe(receive);
        if (!mount.current) {
          throw new Error("无法建立游戏画面。");
        }
        await instance.mount(mount.current);
        if (controller.signal.aborted) {
          return;
        }
        setState(instance.getState());
        setAvailability(instance.getCheckpointAvailability());
        const response = await api.POST("/api/v1/runs/{runId}/events", {
          params: { path: { runId: run.id } },
          body: { event: "running" },
        });
        if (response.error) {
          throw new Error(response.error.message);
        }
      } catch (failure) {
        if (!controller.signal.aborted) {
          setError(failure instanceof Error ? failure.message : "启动失败。");
        }
      }
    }
    void start();
    return () => {
      unsubscribe?.();
      runtime.current = null;
      if (!instance) {
        controller.abort();
        return;
      }
      const mounted = ["RUNNING", "PAUSED", "CHECKPOINTING"].includes(
        instance.getState(),
      );
      if (!mounted) {
        controller.abort();
      }
      void disposeRuntime(instance, callbacks.current.onUnmountSnapshot)
        .catch((failure) =>
          window.dispatchEvent(
            new CustomEvent("retrom:save-failure", {
              detail: {
                userId,
                message:
                  failure instanceof Error
                    ? failure.message
                    : "退出存档未能保存。",
              },
            }),
          ),
        )
        .finally(() => controller.abort());
    };
  }, [run, restore, userId, contentLoading]);
  return { mount, runtime, state, error, availability, step };
}
async function hash(bytes: Uint8Array) {
  const digest = await crypto.subtle.digest(
    "SHA-256",
    Uint8Array.from(bytes).buffer,
  );
  return [...new Uint8Array(digest)]
    .map((b) => b.toString(16).padStart(2, "0"))
    .join("");
}
