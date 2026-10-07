"use client";
import { useToast } from "@/components/toast-provider";
import Image from "next/image";
import { useEffect, useRef, useState } from "react";
import { api, ApiError } from "@/lib/api/client";
import type { Save } from "@/lib/api/types";
import { LaunchButton } from "@/features/player/launch-button";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { AppIcon } from "@/components/app-icon";
import { BrowserTime } from "@/components/browser-time";
const reasons: Record<Save["restoreReason"], string> = {
  "": "",
  game_unavailable: "游戏当前不可用",
  save_unavailable: "存档文件当前不可用",
  core_unavailable: "原核心不可用",
  core_changed: "核心实现已变化",
  content_changed: "游戏内容已变化",
  format_unreadable: "当前核心无法读取存档格式",
};
export function SaveCard({
  save,
  onChange,
  compact = false,
}: {
  save: Save;
  onChange?: () => void;
  compact?: boolean;
}) {
  const [dialog, setDialog] = useState<"rename" | "delete" | null>(null);
  const [menu, setMenu] = useState(false);
  const menuButton = useRef<HTMLButtonElement>(null);
  const menuPanel = useRef<HTMLDivElement>(null);
  const [name, setName] = useState(save.name);
  const { notify } = useToast();
  const [busy, setBusy] = useState(false);
  useEffect(() => {
    if (!menu) { return; }
    function outside(event: Event) {
      const target = event.target;
      if (target instanceof Node &&
        (menuButton.current?.contains(target) || menuPanel.current?.contains(target))) {
        return;
      }
      setMenu(false);
    }
    function escape(event: KeyboardEvent) {
      if (event.key !== "Escape" || event.defaultPrevented || event.isComposing) { return; }
      event.preventDefault();
      event.stopPropagation();
      setMenu(false);
      menuButton.current?.focus();
    }
    document.addEventListener("pointerdown", outside, true);
    document.addEventListener("click", outside, true);
    document.addEventListener("keydown", escape);
    return () => {
      document.removeEventListener("pointerdown", outside, true);
      document.removeEventListener("click", outside, true);
      document.removeEventListener("keydown", escape);
    };
  }, [menu]);
  async function mutate() {
    setBusy(true);
    const params = { path: { saveId: save.id } };
    try {
      const response =
        dialog === "delete"
          ? await api.DELETE("/api/v1/saves/{saveId}", {
              params,
              body: { version: save.version },
            })
          : await api.PATCH("/api/v1/saves/{saveId}", {
              params,
              body: { version: save.version, name },
            });
      if (response.error) {
        throw new ApiError(response.error.code, response.error.message, response.response.status);
      }
      notify({ tone: "good", message: dialog === "delete" ? "存档已删除" : "存档名称已更新" });
      setDialog(null);
      onChange?.();
    } catch (failure) {
      notify({ tone: "bad", message: failure instanceof Error ? failure.message : "操作失败，请重试。" });
    } finally {
      setBusy(false);
    }
  }
  const prefix = compact ? "game-detail-save" : "save-library";
  return (
    <article className={`${prefix}-card`}>
      <SaveImage save={save} compact={compact} />
      <div
        className={compact ? "game-detail-save-body" : "save-library-card-body"}
      >
        <div className="save-library-title-row">
          <BrowserTime value={save.updatedAtMs} />
          {!compact ? (
            <>
              <button
                ref={menuButton}
                className="save-library-menu-button"
                aria-label={`${save.name}的更多操作`}
                aria-haspopup="menu"
                aria-expanded={menu}
                onClick={() => setMenu((open) => !open)}
              >
                <AppIcon name="more" />
              </button>
              {menu ? (
                <div ref={menuPanel} className="save-library-menu" role="menu">
                  <button
                    role="menuitem"
                    onClick={() => {
                      setMenu(false);
                      setDialog("rename");
                    }}
                  >
                    <AppIcon name="pencil" />
                    重命名
                  </button>
                  <button
                    role="menuitem"
                    onClick={() => {
                      setMenu(false);
                      setDialog("delete");
                    }}
                  >
                    <AppIcon name="x" />
                    删除存档
                  </button>
                </div>
              ) : null}
            </>
          ) : null}
        </div>
        <div className="save-library-card-meta">
          <span>{save.kind === "checkpoint" ? "即时存档" : "游戏内存档"}</span>
          <span>{save.extinfo.coreId}</span>
        </div>
        <p className="save-library-custom-name" title={save.name}>
          {save.name}
        </p>
        <div className="save-library-resume">
          <LaunchButton
            gameId={save.game.id}
            coreId={save.extinfo.coreId}
            saveId={save.id}
            disabled={!save.restorable}
          >
            从这里继续
          </LaunchButton>
        </div>
        {!save.restorable ? (
          <p className="save-library-reason">{reasons[save.restoreReason]}</p>
        ) : null}
        {save.kind === "game_save" ? (
          <p className="workspace-note">恢复后使用游戏内读档菜单。</p>
        ) : null}
      </div>
      <ConfirmDialog
        open={dialog !== null}
        title={dialog === "delete" ? "删除存档" : "存档名称"}
        description={
          dialog === "delete" ? "删除后无法继续使用这份进度。" : undefined
        }
        tone={dialog === "delete" ? "danger" : "default"}
        busy={busy}
        onCancel={() => setDialog(null)}
        onConfirm={() => void mutate()}
      >
        {dialog === "rename" ? (
          <label className="field">
            名称
            <input
              value={name}
              onChange={(event) => setName(event.target.value)}
            />
          </label>
        ) : null}
      </ConfirmDialog>
    </article>
  );
}

function SaveImage({ save, compact }: { save: Save; compact: boolean }) {
  return (
    <div className={compact ? "game-detail-save-media" : "save-library-shot"}>
      {save.screenshotUrl ? (
        <Image
          src={save.screenshotUrl}
          alt={`${save.name}的截图`}
          fill
          sizes="280px"
          unoptimized
        />
      ) : (
        <span className="save-screenshot-placeholder">暂无截图</span>
      )}
      {!save.restorable ? (
        <span
          className={
            compact ? "game-detail-save-blocked" : "save-library-blocked"
          }
        >
          当前不可恢复
        </span>
      ) : null}
      {!compact ? (
        <span className="save-library-size">
          {(save.sizeBytes / 1024).toFixed(1)} KiB
        </span>
      ) : null}
    </div>
  );
}
