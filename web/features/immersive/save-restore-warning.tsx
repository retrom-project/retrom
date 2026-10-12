import { StatusBadge } from "@/components/ui";
import { AppIcon } from "@/components/app-icon";
import type { Save } from "@/lib/api/types";
import styles from "./library.module.css";

const restoreMessages: Record<Save["restoreReason"], string> = {
  "": "", game_unavailable: "游戏当前不可用", save_unavailable: "存档文件当前不可用",
  core_unavailable: "保存时的核心当前不可用",
  content_changed: "游戏文件与创建存档时不一致，无法恢复", format_unreadable: "当前核心无法读取此存档格式",
};

export function SaveRestoreWarning({ save, thumbnail = false }: { save: Save; thumbnail?: boolean }) {
  if (save.restorable) {
    return thumbnail && save.restoreWarning === "core_changed"
      ? <span className={styles.saveVersionWarning}><StatusBadge tone="warn">核心已变化</StatusBadge></span> : null;
  }
  const message = restoreMessages[save.restoreReason] || "存档当前不可加载";
  return <span className={`${styles.saveUnavailable} ${thumbnail ? styles.saveUnavailableThumbnail : styles.saveUnavailablePreview}`}
    role={thumbnail ? "img" : "status"} aria-label={thumbnail ? `存档不可加载：${message}` : undefined}>
    <AppIcon name="ban" />
    {!thumbnail ? <><strong>存档无法加载</strong><span>{message}</span></> : null}
  </span>;
}
