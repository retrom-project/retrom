"use client";
import { FeedbackBanner, StatusBadge } from "@/components/ui";
import { usePwa } from "./pwa-provider";

/** Connection and update notices float above the page so they never push its layout down. */
export function PwaNotice() {
  const pwa = usePwa();
  if (!pwa) { return null; }
  if (!pwa.online) {
    return <div className="pwa-floating"><FeedbackBanner tone="info">当前网络已断开。启动游戏和同步存档需要恢复连接。</FeedbackBanner></div>;
  }
  if (!pwa.update) { return null; }
  return <div className="pwa-floating"><FeedbackBanner tone="info"><div className="pwa-notice">
    <span>{pwa.updateMessage || "Retrom 有新版本，可在结束游玩后刷新更新。"}</span>
    <button className="button secondary" disabled={pwa.updating} onClick={() => void pwa.applyUpdate()}>{pwa.updating ? "正在更新…" : "刷新更新"}</button>
  </div></FeedbackBanner></div>;
}

export function PwaSettings() {
  const pwa = usePwa();
  if (!pwa) { return null; }
  const guide = installationGuide(pwa);
  return <section className="panel pwa-settings" aria-labelledby="pwa-heading">
    <div className="panel-head"><div><h2 id="pwa-heading">Retrom 应用</h2><p>从桌面或主屏幕打开自己的游戏库。</p></div>
      {pwa.installed ? <StatusBadge tone="good">已安装</StatusBadge> : null}
    </div>
    <div className="panel-body stack">
      <p>{guide}</p>
      {!pwa.installed && pwa.secure && pwa.available ? <div><button className="button" disabled={pwa.busy} onClick={() => void pwa.install()}>{pwa.busy ? "正在打开安装提示…" : "安装 Retrom"}</button></div> : null}
      {pwa.message ? <p role="status">{pwa.message}</p> : null}
      <p className="pwa-description">游玩和同步存档需要连接服务器。安装后仍使用当前游戏库。</p>
      {pwa.secure && pwa.offlineSupported ? <div className="pwa-notice">
        <span>{pwa.error || (pwa.update ? "新版本已就绪。" : pwa.ready ? "离线提示已就绪。" : "正在准备离线提示…")}</span>
        <button className="button secondary" disabled={!pwa.online} onClick={() => void pwa.checkUpdate()}>检查更新</button>
      </div> : null}
      {pwa.secure && !pwa.offlineSupported ? <p>当前浏览器不支持离线提示，仍可联网使用。</p> : null}
    </div>
  </section>;
}

function installationGuide(pwa: NonNullable<ReturnType<typeof usePwa>>) {
  if (pwa.installed) { return "可以从设备上的 Retrom 图标打开应用。"; }
  if (!pwa.secure) { return "当前连接不支持安装，请通过 HTTPS 地址打开 Retrom。"; }
  if (pwa.ios) { return "在浏览器中打开分享菜单，选择“添加到主屏幕”；如有“作为 Web App 打开”选项，请将其开启。"; }
  if (pwa.available) { return "安装后以独立窗口打开，方便随时回来继续游玩。"; }
  return "如浏览器提供“安装应用”或“添加到主屏幕”，可通过浏览器菜单安装。";
}
