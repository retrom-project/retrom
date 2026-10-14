"use client";
import { useEffect, useState } from "react";

export type InstallPrompt = Event & {
  prompt(): Promise<void>;
  userChoice: Promise<{ outcome: "accepted" | "dismissed" }>;
};

export function useInstallation() {
  const [installed, setInstalled] = useState(false);
  const [appWindow, setAppWindow] = useState<boolean | null>(null);
  const [ios, setIOS] = useState(false);
  const [secure, setSecure] = useState(true);
  const [offlineSupported, setOfflineSupported] = useState(true);
  const [prompt, setPrompt] = useState<InstallPrompt | null>(null);
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState("");
  useEffect(() => {
    const mode = window.matchMedia("(display-mode: standalone)");
    const update = () => {
      const navigatorIOS = navigator as Navigator & { standalone?: boolean };
      const standalone = mode.matches || navigatorIOS.standalone === true;
      setInstalled((current) => current || standalone);
      // Fullscreen temporarily replaces the display mode of an app window.
      // Keep its identity for this document, independently of installation in a browser tab.
      setAppWindow((current) => current === true || standalone);
      setIOS(/iPad|iPhone|iPod/u.test(navigator.userAgent) || (navigator.platform === "MacIntel" && navigator.maxTouchPoints > 1));
      setSecure(window.isSecureContext);
      setOfflineSupported("serviceWorker" in navigator);
    };
    const available = (event: Event) => {
      event.preventDefault();
      setPrompt(event as InstallPrompt);
      setMessage("");
    };
    const installedApp = () => { setInstalled(true); setPrompt(null); setMessage(""); };
    const timer = setTimeout(update, 0);
    mode.addEventListener("change", update);
    window.addEventListener("beforeinstallprompt", available);
    window.addEventListener("appinstalled", installedApp);
    return () => {
      clearTimeout(timer);
      mode.removeEventListener("change", update);
      window.removeEventListener("beforeinstallprompt", available);
      window.removeEventListener("appinstalled", installedApp);
    };
  }, []);
  async function install() {
    if (!prompt || busy) { return; }
    setBusy(true);
    try {
      await prompt.prompt();
      const choice = await prompt.userChoice;
      setMessage(choice.outcome === "accepted" ? "请完成浏览器的安装提示。" : "已取消安装，仍可在浏览器中继续使用。");
    } catch { setMessage("未能打开安装提示，请尝试浏览器菜单中的安装选项。"); }
    finally { setPrompt(null); setBusy(false); }
  }
  return { installed, appWindow, ios, secure, offlineSupported, available: Boolean(prompt), busy, message, install };
}
