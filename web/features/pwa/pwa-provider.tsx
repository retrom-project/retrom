"use client";
import { createContext, useContext, useEffect, useRef, useState } from "react";
import type { ReactNode } from "react";
import { observeWorker } from "./worker-client";
import type { WorkerState } from "./worker-client";
import { useInstallation } from "./use-installation";

type PwaState = ReturnType<typeof useInstallation> & WorkerState & {
  online: boolean;
  updating: boolean;
  updateMessage: string;
  applyUpdate(): Promise<void>;
  checkUpdate(): Promise<void>;
};
const Pwa = createContext<PwaState | null>(null);
export function PwaProvider({ children }: { children: ReactNode }) {
  const installation = useInstallation();
  const [worker, setWorker] = useState<WorkerState>({ ready: false, update: false, error: "" });
  const [online, setOnline] = useState(true);
  const [updating, setUpdating] = useState(false);
  const [updateMessage, setUpdateMessage] = useState("");
  const observer = useRef<ReturnType<typeof observeWorker> | null>(null);
  useEffect(() => {
    const connection = () => setOnline(navigator.onLine);
    const timer = setTimeout(connection, 0);
    window.addEventListener("online", connection);
    window.addEventListener("offline", connection);
    if (window.isSecureContext && "serviceWorker" in navigator) {
      observer.current = observeWorker(setWorker);
    }
    return () => {
      clearTimeout(timer);
      window.removeEventListener("online", connection);
      window.removeEventListener("offline", connection);
      observer.current?.dispose();
      observer.current = null;
    };
  }, []);
  async function applyUpdate() {
    if (!observer.current || updating) { return; }
    setUpdating(true);
    setUpdateMessage("");
    try {
      const result = await observer.current.apply();
      if (result === "playing" || /^\/(?:play|immersive)(?:\/|$)/u.test(location.pathname)) {
        setUpdateMessage("请先结束所有 Retrom 窗口中的游戏并返回普通界面，再更新。");
      } else {
        location.reload();
      }
    } catch (error) {
      setUpdateMessage(error instanceof Error ? error.message : "更新暂不可用，请稍后重试。");
    } finally { setUpdating(false); }
  }
  async function checkUpdate() {
    setUpdateMessage("");
    await observer.current?.check();
  }
  return <Pwa.Provider value={{ ...installation, ...worker, online, updating, updateMessage, applyUpdate, checkUpdate }}>{children}</Pwa.Provider>;
}
export function usePwa() { return useContext(Pwa); }
