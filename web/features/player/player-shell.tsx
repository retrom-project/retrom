"use client";

import Link from "next/link";
import { useCallback, useEffect, useRef, useState } from "react";
import { captureManualState, mountEmulatorJS, type EmulatorInstance, type PlayerConfig } from "./adapters/ejs-4.2.3-v1";

type ShellState = "loading" | "running" | "error";

function base64(bytes: ArrayBuffer) {
  let value = "";
  for (const byte of new Uint8Array(bytes)) value += String.fromCharCode(byte);
  return btoa(value);
}

function ensureDirectory(fs: NonNullable<NonNullable<EmulatorInstance["gameManager"]>["FS"]>, filePath: string) {
  const segments = filePath.split("/").filter(Boolean);
  let current = "";
  for (const segment of segments.slice(0, -1)) {
    current += `/${segment}`;
    if (!fs.analyzePath(current).exists) fs.mkdir(current);
  }
}

export function PlayerShell({ launchId }: { launchId: string }) {
  const stage = useRef<HTMLDivElement>(null);
  const frameRef = useRef<HTMLIFrameElement>(null);
  const emulator = useRef<EmulatorInstance | undefined>(undefined);
  const [state, setState] = useState<ShellState>("loading");
  const [message, setMessage] = useState("正在验证运行快照…");
  const [warnings, setWarnings] = useState<string[]>([]);
  const [saveMessage, setSaveMessage] = useState("");
  const returnTo = useRef("/library");
  const sequence = useRef(0);
  const started = useRef(false);
  const finishing = useRef(false);
  const heartbeat = useRef<number | null>(null);
  const persistentSequence = useRef(0);
  const persistentQueue = useRef(Promise.resolve());
  const persistentConflict = useRef<Uint8Array | null>(null);
  const [hasPersistentConflict, setHasPersistentConflict] = useState(false);

  const sendEvent = useCallback(async (kind: "start" | "heartbeat" | "finish") => {
    if (kind === "heartbeat" && !started.current) throw new Error("PLAY_SESSION_NOT_STARTED");
    const next = kind === "start" || kind === "finish" && !started.current ? 0 : sequence.current + 1;
    const response = await fetch(`/runtime/launches/${launchId}/${kind}`, {
      method: "POST",
      credentials: "same-origin",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        clientSequence: next,
        clientObservedAtMs: Date.now(),
        previousInterval: kind === "start" || kind === "finish" && !started.current ? null : {
          running: true,
          visible: document.visibilityState === "visible",
          paused: emulator.current?.paused === true
        }
      })
    });
    if (!response.ok) throw new Error("PLAY_SESSION_EVENT_FAILED");
    sequence.current = next;
    if (kind === "start") started.current = true;
    if (kind === "finish") finishing.current = true;
  }, [launchId]);

  const uploadPersistent = useCallback((bytes: Uint8Array, event: "AUTO_INTERVAL" | "MANUAL_EXPORT" | "EXIT") => {
    const stableBytes = new Uint8Array(bytes);
    persistentQueue.current = persistentQueue.current.then(async () => {
      if (persistentConflict.current) return;
      const digest = await crypto.subtle.digest("SHA-256", stableBytes);
      const next = persistentSequence.current + 1;
      const idempotencyKey = crypto.randomUUID();
      let response: Response | undefined;
      for (let attempt = 0; attempt < 2; attempt += 1) {
        try {
          response = await fetch(`/runtime/launches/${launchId}/persistent-save`, {
            method: "PUT",
            credentials: "same-origin",
            headers: {
              "Content-Type": "application/octet-stream",
              "Content-Digest": `sha-256=:${base64(digest)}:`,
              "Idempotency-Key": idempotencyKey,
              "X-Retrom-Save-Sequence": String(next),
              "X-Retrom-Save-Event": event
            },
            body: stableBytes
          });
          if (response.status < 500) break;
        } catch {
          if (attempt === 1) throw new Error("PERSISTENT_SAVE_NETWORK_FAILED");
        }
      }
      if (!response) throw new Error("PERSISTENT_SAVE_NETWORK_FAILED");
      if (!response.ok) {
        if (response.status === 409) {
          persistentConflict.current = stableBytes;
          setHasPersistentConflict(true);
          setSaveMessage("服务器存档已由另一会话推进；当前进度未覆盖，请先下载本地副本再退出重启。");
        } else {
          setSaveMessage("持久存档同步失败，最后有效版本仍被保留。");
        }
        throw new Error("PERSISTENT_SAVE_FAILED");
      }
      persistentSequence.current = next;
      setSaveMessage("持久进度已同步");
    }).catch(() => undefined);
  }, [launchId]);

  const uploadManualState = useCallback(async (payload: { screenshot: Blob; format: string; state: Uint8Array }) => {
    if (!payload.screenshot.size || !payload.state.byteLength) {
      setSaveMessage("状态或截图为空，未创建存档。");
      return;
    }
    const form = new FormData();
    form.append("metadata", new Blob([JSON.stringify({ name: `手动存档 ${new Date().toLocaleString("zh-CN")}` })], { type: "application/json" }));
    const stateBytes = new Uint8Array(payload.state).slice().buffer;
    form.append("state", new Blob([stateBytes], { type: "application/octet-stream" }), `state.${payload.format || "bin"}`);
    form.append("screenshot", payload.screenshot, `screenshot.${payload.format || "png"}`);
    const response = await fetch(`/runtime/launches/${launchId}/save-states`, {
      method: "POST", credentials: "same-origin",
      headers: { "Idempotency-Key": crypto.randomUUID() }, body: form
    });
    setSaveMessage(response.ok ? "手动存档和截图已保存" : "手动存档失败，服务器未创建不完整记录");
  }, [launchId]);

  async function exit() {
    try {
      const manager = emulator.current?.gameManager;
      const path = manager?.getSaveFilePath?.();
      if (!persistentConflict.current && path && manager?.FS?.analyzePath(path).exists) {
        const bytes = await manager.getSaveFile?.();
        if (bytes?.byteLength) uploadPersistent(bytes, "EXIT");
      }
      await persistentQueue.current;
      await sendEvent("finish");
    } catch { /* expiry is already a terminal server state */ }
    if (document.fullscreenElement) await document.exitFullscreen().catch(() => undefined);
    window.location.assign(returnTo.current);
  }

  function downloadConflictingSave() {
    const bytes = persistentConflict.current;
    if (!bytes) return;
    const copy = new Uint8Array(bytes).slice().buffer;
    const url = URL.createObjectURL(new Blob([copy], { type: "application/octet-stream" }));
    const anchor = document.createElement("a");
    anchor.href = url;
    anchor.download = `retrom-${launchId}-local-save.bin`;
    anchor.click();
    URL.revokeObjectURL(url);
  }

  useEffect(() => {
    const controller = new AbortController();
    let cleanup: (() => void) | undefined;
    async function bootstrap() {
      try {
        setMessage("正在加载 Core、ROM 与依赖配置…");
        const response = await fetch(`/runtime/launches/${launchId}/config`, { credentials: "same-origin", cache: "no-store", signal: controller.signal });
        if (!response.ok) throw new Error(`LAUNCH_CONFIG_${response.status}`);
        const config = await response.json() as PlayerConfig;
        returnTo.current = config.returnTo;
        setWarnings(config.warnings ?? []);

        const persistentResponse = await fetch(config.persistentSaveUrl, { credentials: "same-origin", cache: "no-store", signal: controller.signal });
        if (!persistentResponse.ok && persistentResponse.status !== 204) throw new Error("LAUNCH_PERSISTENT_SAVE_LOAD_FAILED");
        const contentLength = Number(persistentResponse.headers.get("content-length") ?? "0");
        if (contentLength > 64 * 1024 * 1024) throw new Error("LAUNCH_PERSISTENT_SAVE_TOO_LARGE");
        const persistentBytes = persistentResponse.status === 204 ? null : new Uint8Array(await persistentResponse.arrayBuffer());
        if (persistentBytes && persistentBytes.byteLength > 64 * 1024 * 1024) throw new Error("LAUNCH_PERSISTENT_SAVE_TOO_LARGE");

        if (!stage.current || !frameRef.current) return;
        const frame = frameRef.current;
        const frameWindow = frame.contentWindow;
        const frameDocument = frame.contentDocument;
        if (!frameWindow || !frameDocument) throw new Error("PLAYER_FRAME_UNAVAILABLE");
        frameDocument.documentElement.lang = "zh-CN";
        const style = frameDocument.createElement("style");
        style.textContent = "html,body,#game{width:100%;height:100%;margin:0;overflow:hidden;background:#05060a} canvas{max-width:100%;max-height:100%}";
        frameDocument.head.append(style);
        const target = frameDocument.createElement("div");
        target.id = "game";
        frameDocument.body.append(target);

        let mountedSaveFS: NonNullable<NonNullable<EmulatorInstance["gameManager"]>["FS"]> | undefined;
        cleanup = mountEmulatorJS(config, target, {
          onReady: (instance) => {
            emulator.current = instance;
            instance.on("saveState", () => undefined);
            instance.on("saveDatabaseLoaded", () => {
              const fs = instance.gameManager?.FS;
              if (!fs) { setState("error"); setMessage("LAUNCH_PERSISTENT_SAVE_FS_UNAVAILABLE"); return; }
              mountedSaveFS = fs;
            });
            instance.on("saveSaveFiles", (value: unknown) => {
              if (value instanceof Uint8Array && value.byteLength) uploadPersistent(value, "AUTO_INTERVAL");
            });
            instance.on("exit", () => { void sendEvent("finish"); });
          },
          onGameStart: () => {
            const manager = emulator.current?.gameManager;
            const savePath = manager?.getSaveFilePath?.();
            if (!manager || !mountedSaveFS || !savePath) { setState("error"); setMessage("LAUNCH_PERSISTENT_SAVE_PATH_UNAVAILABLE"); return; }
            manager.toggleMainLoop?.(false);
            ensureDirectory(mountedSaveFS, savePath);
            if (persistentBytes) mountedSaveFS.writeFile(savePath, persistentBytes);
            else if (mountedSaveFS.analyzePath(savePath).exists) mountedSaveFS.unlink(savePath);
            manager.loadSaveFiles?.();
            manager.toggleMainLoop?.(true);
            void sendEvent("start").then(() => {
              setState("running");
              heartbeat.current = window.setInterval(() => { void sendEvent("heartbeat"); }, 30_000);
            }).catch(() => {
              setState("error");
              setMessage("PLAY_SESSION_EVENT_FAILED");
            });
          },
          onSaveState: (payload) => { void uploadManualState(payload); },
          onSaveSave: (payload) => { if (payload.save.byteLength) uploadPersistent(payload.save, "MANUAL_EXPORT"); }
        }, frameWindow);
      } catch (error) {
        if (controller.signal.aborted) return;
        const code = error instanceof Error ? error.message : "启动失败";
        setMessage(code === "LAUNCH_CONFIG_401" ? "启动会话不可用，请从游戏详情或存档重新开始。" : code);
        setState("error");
      }
    }
    void bootstrap();
    return () => {
      controller.abort(); cleanup?.();
      if (heartbeat.current !== null) window.clearInterval(heartbeat.current);
    };
  }, [launchId, sendEvent, uploadManualState, uploadPersistent]);

  useEffect(() => {
    const handlePageHide = () => {
      if (finishing.current) return;
      const wasStarted = started.current;
      const next = wasStarted ? sequence.current + 1 : 0;
      void fetch(`/runtime/launches/${launchId}/finish`, {
        method: "POST",
        credentials: "same-origin",
        keepalive: true,
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          clientSequence: next,
          clientObservedAtMs: Date.now(),
          previousInterval: wasStarted ? { running: true, visible: document.visibilityState === "visible", paused: emulator.current?.paused === true } : null
        })
      });
      finishing.current = true;
    };
    window.addEventListener("pagehide", handlePageHide);
    return () => window.removeEventListener("pagehide", handlePageHide);
  }, [launchId]);

  async function saveManualState() {
    const current = emulator.current;
    if (!current) return;
    setSaveMessage("正在保存进度…");
    try {
      await uploadManualState(await captureManualState(current));
    } catch {
      setSaveMessage("无法从模拟器读取完整状态和截图");
    }
  }

  return <main className="player-shell"><header className="player-toolbar"><strong>Retrom Player · {launchId.slice(0, 8)}</strong><div aria-live="polite" className="player-save-status">{warnings.includes("BIOS_HASH_WARNING") ? <span>BIOS Hash 与目录期望不一致，已按 Warning 继续运行。</span> : null}{saveMessage}{hasPersistentConflict ? <button type="button" onClick={downloadConflictingSave}>下载当前存档</button> : null}</div><div className="player-actions"><button type="button" disabled={state !== "running"} onClick={() => void saveManualState()}>保存进度</button><button type="button" onClick={() => void document.documentElement.requestFullscreen()}>全屏</button><button type="button" onClick={() => void exit()}>退出游戏</button></div></header><div className="player-stage" ref={stage}><iframe ref={frameRef} title="Retrom EmulatorJS Player" className="player-frame" src="about:blank" />{state !== "running" ? <div className="player-loading">{state === "loading" ? <i /> : null}<strong>{message}</strong><p>{state === "error" ? <><span>凭据可能已过期或依赖不兼容。</span> <Link href="/library">返回游戏库</Link></> : "页面会在验证和存档预取后自动开始，无需再次点击。"}</p></div> : null}</div></main>;
}
