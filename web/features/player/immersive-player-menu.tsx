import type { ImmersivePlayerOverlay } from "./use-immersive-player";
import type { ImmersiveMenuSelection } from "./immersive-player-menu-model";

type Props = {
  overlay: ImmersivePlayerOverlay;
  saveAvailable: boolean;
  onCancel: () => void;
  onSelect: (selected: ImmersiveMenuSelection) => void;
  onConfirm: () => void;
};

export function ImmersivePlayerMenu({ overlay, saveAvailable, onCancel, onSelect, onConfirm }: Props) {
  if (overlay.kind === "closed") {return null;}
  if (overlay.kind === "reconnect") {
    return <section className="immersive-player-overlay" role="alertdialog" aria-modal="true" aria-labelledby="immersive-reconnect-title">
      <div className="immersive-player-panel">
        <p className="immersive-player-eyebrow">控制器连接已中断</p>
        <h1 id="immersive-reconnect-title">请重新连接手柄</h1>
        <p>{overlay.ready ? "手柄已就绪，按 A 继续" : "触碰任意按键认领手柄，然后松开全部输入"}</p>
      </div>
    </section>;
  }
  if (overlay.kind === "closing") {
    return <section className="immersive-player-overlay" aria-live="polite"><div className="immersive-player-panel"><p>请松开手柄按键…</p></div></section>;
  }
  return <section className="immersive-player-overlay" role="dialog" aria-modal="true" aria-labelledby="immersive-player-menu-title">
    <div className="immersive-player-panel">
      <p className="immersive-player-eyebrow">游戏已暂停</p>
      <h1 id="immersive-player-menu-title">游戏菜单</h1>
      <p>可以在这里创建存档；退出游戏不会自动保存当前进度。</p>
      {overlay.notice ? <p className="immersive-player-notice" role="status">{overlay.notice}</p> : null}
      {overlay.error ? <p className="immersive-player-error" role="alert">{overlay.error}</p> : null}
      <div className="immersive-player-actions">
        <button type="button" disabled={overlay.pending} className={overlay.selected === 0 ? "is-selected" : ""} aria-current={overlay.selected === 0} onFocus={() => onSelect(0)} onClick={onCancel}>取消</button>
        <button type="button" disabled={overlay.pending || !saveAvailable} className={overlay.selected === 1 ? "is-selected" : ""} aria-current={overlay.selected === 1} aria-describedby={!saveAvailable ? "immersive-save-unavailable" : undefined} onFocus={() => onSelect(1)} onClick={() => confirmMenuItem(1, onSelect, onConfirm)}>创建存档</button>
        <button type="button" disabled={overlay.pending} className={overlay.selected === 2 ? "is-selected" : ""} aria-current={overlay.selected === 2} onFocus={() => onSelect(2)} onClick={() => confirmMenuItem(2, onSelect, onConfirm)}>退出游戏</button>
      </div>
      {!saveAvailable ? <p id="immersive-save-unavailable" className="immersive-player-unavailable">当前运行方式无法创建可恢复存档。</p> : null}
      <small>A 确认 · B 取消</small>
    </div>
  </section>;
}

function confirmMenuItem(selected: ImmersiveMenuSelection, onSelect: Props["onSelect"], onConfirm: Props["onConfirm"]) {
  onSelect(selected);
  onConfirm();
}
