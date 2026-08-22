"use client";

import { forwardRef, useCallback, useEffect, useImperativeHandle, useRef, useState, type RefObject } from "react";
import { AppIcon } from "@/components/app-icon";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { useAuth } from "@/features/auth/auth-provider";
import {
  createFavoriteFolder,
  FavoriteAPIError,
  loadFavorites,
  putFavorite,
  replaceFavoriteFolders,
  restoreFavorites,
  unfavoriteGames,
  type FavoriteFolder,
  type FavoriteReference,
  type UnfavoriteResult,
} from "./favorite-api";
import { FolderNameDialog, FolderPickerDialog } from "./folder-dialogs";

type Notice = { message: string; undo?: UnfavoriteResult["items"]; offerManage?: boolean };

export type FavoriteActionsHandle = {
  openFolderPicker: (anchor: HTMLElement, resolveReturnTarget?: () => HTMLElement | null) => void;
};

type FavoriteActionsProps = {
  gameId: string;
  title: string;
  initialFavorite: FavoriteReference | null;
  variant?: "card" | "favorite-card" | "detail";
  showManageButton?: boolean;
  onChange?: (favorite: FavoriteReference | null) => void;
};

function messageFor(error: unknown) {
  if (error instanceof FavoriteAPIError) {
    if (error.code === "FAVORITE_FOLDER_NAME_CONFLICT") {return "已经存在同名收藏夹";}
    if (error.code === "RESOURCE_VERSION_CONFLICT") {return "收藏夹已在其他页面修改，请刷新后重试";}
    return `${error.message}（${error.code}）`;
  }
  return "收藏操作失败，请重试";
}

export const FavoriteActions = forwardRef<FavoriteActionsHandle, FavoriteActionsProps>(function FavoriteActions({
  gameId, title, initialFavorite, variant = "card", showManageButton = true, onChange,
}, ref) {
  const { authenticatedFetch } = useAuth();
  const [favorite, setFavorite] = useState(initialFavorite);
  const [folders, setFolders] = useState<FavoriteFolder[]>([]);
  const [busy, setBusy] = useState(false);
  const [confirming, setConfirming] = useState(false);
  const [picker, setPicker] = useState(false);
  const [pickerAnchor, setPickerAnchor] = useState<HTMLElement | null>(null);
  const [creating, setCreating] = useState(false);
  const [createError, setCreateError] = useState("");
  const [notice, setNotice] = useState<Notice | null>(null);
  const heartButton = useRef<HTMLButtonElement>(null);
  const internalManageButton = useRef<HTMLButtonElement>(null);
  const pickerReturnTarget = useRef<(() => HTMLElement | null) | null>(null);
  useEffect(() => {
    if (!notice) {return;}
    const timer = window.setTimeout(() => setNotice(null), 2_000);
    return () => window.clearTimeout(timer);
  }, [notice]);

  const acceptFavorite = useCallback((next: FavoriteReference | null) => {
    setFavorite(next);
    onChange?.(next);
  }, [onChange]);

  async function addFavorite() {
    setBusy(true);
    try {
      const { data } = await putFavorite(authenticatedFetch, gameId);
      acceptFavorite({ favoritedAtMs: data.favoritedAtMs, folderIds: data.folderIds });
      setNotice({ message: `已收藏“${title}”`, offerManage: true });
    } catch (error) { setNotice({ message: messageFor(error) }); }
    finally { setBusy(false); }
  }

  async function removeFavorite() {
    setBusy(true);
    try {
      const { data } = await unfavoriteGames(authenticatedFetch, [gameId]);
      acceptFavorite(null);
      setConfirming(false);
      setNotice({ message: `已取消收藏“${title}”`, undo: data.items });
    } catch (error) { setNotice({ message: messageFor(error) }); }
    finally { setBusy(false); }
  }

  async function undo() {
    if (!notice?.undo?.length) {return;}
    setBusy(true);
    try {
      await restoreFavorites(authenticatedFetch, notice.undo);
      const { data } = await putFavorite(authenticatedFetch, gameId);
      acceptFavorite({ favoritedAtMs: data.favoritedAtMs, folderIds: data.folderIds });
      setNotice({ message: "已恢复收藏" });
    } catch (error) { setNotice({ message: messageFor(error) }); }
    finally { setBusy(false); }
  }

  const openPicker = useCallback(async (
    anchor?: HTMLElement | null,
    resolveReturnTarget?: () => HTMLElement | null,
  ) => {
    setBusy(true);
    setPickerAnchor(anchor ?? internalManageButton.current ?? heartButton.current);
    pickerReturnTarget.current = resolveReturnTarget ?? null;
    try {
      const { data } = await loadFavorites(authenticatedFetch, "limit=1");
      setFolders(data.folders);
      setPicker(true);
    } catch (error) { setNotice({ message: messageFor(error) }); }
    finally { setBusy(false); }
  }, [authenticatedFetch]);

  useImperativeHandle(ref, () => ({
    openFolderPicker: (anchor, resolveReturnTarget) => { void openPicker(anchor, resolveReturnTarget); },
  }), [openPicker]);

  function closePicker() {
    setPicker(false);
    setPickerAnchor(null);
  }

  const resolvePickerReturnFocus = useCallback(() => {
    const resolved = pickerReturnTarget.current?.();
    if (resolved?.isConnected) {return resolved;}
    if (pickerAnchor?.isConnected) {return pickerAnchor;}
    return internalManageButton.current ?? heartButton.current;
  }, [pickerAnchor]);

  async function saveFolders(folderIds: string[]) {
    setBusy(true);
    try {
      const { data } = await replaceFavoriteFolders(authenticatedFetch, gameId, folderIds);
      acceptFavorite({ favoritedAtMs: data.favoritedAtMs, folderIds: data.folderIds });
      closePicker();
      setNotice({ message: "收藏夹已更新" });
    } catch (error) { setNotice({ message: messageFor(error) }); }
    finally { setBusy(false); }
  }

  async function createFolder(name: string) {
    setBusy(true); setCreateError("");
    try {
      const { data } = await createFavoriteFolder(authenticatedFetch, name, [gameId]);
      setFolders((current) => [...current, data]);
      const { data: state } = await putFavorite(authenticatedFetch, gameId);
      acceptFavorite({ favoritedAtMs: state.favoritedAtMs, folderIds: state.folderIds });
      setCreating(false);
      setPicker(true);
      setNotice({ message: `已创建“${data.name}”并加入游戏` });
    } catch (error) { setCreateError(messageFor(error)); }
    finally { setBusy(false); }
  }

  const membershipCount = favorite?.folderIds.length ?? 0;
  return <FavoriteActionsView {...{
    busy, confirming, createError, creating, favorite, folders, heartButton, internalManageButton,
    membershipCount, notice, picker, pickerAnchor, resolvePickerReturnFocus, showManageButton, title, variant,
  }}
    onAdd={() => void addFavorite()}
    onCloseConfirm={() => setConfirming(false)}
    onCloseCreate={() => {setCreating(false); setPicker(true);}}
    onCloseNotice={() => setNotice(null)}
    onClosePicker={closePicker}
    onConfirmRemove={() => void removeFavorite()}
    onCreateFolder={(name) => void createFolder(name)}
    onManage={(anchor) => void openPicker(anchor)}
    onOpenCreate={() => {setPicker(false); setCreating(true);}}
    onSaveFolders={(folderIds) => void saveFolders(folderIds)}
    onStartRemove={() => setConfirming(true)}
    onUndo={() => void undo()}
  />;
});

type FavoriteActionsViewProps = {
  busy: boolean;
  confirming: boolean;
  createError: string;
  creating: boolean;
  favorite: FavoriteReference | null;
  folders: FavoriteFolder[];
  heartButton: RefObject<HTMLButtonElement | null>;
  internalManageButton: RefObject<HTMLButtonElement | null>;
  membershipCount: number;
  notice: Notice | null;
  picker: boolean;
  pickerAnchor: HTMLElement | null;
  resolvePickerReturnFocus: () => HTMLElement | null;
  showManageButton: boolean;
  title: string;
  variant: NonNullable<FavoriteActionsProps["variant"]>;
  onAdd: () => void;
  onCloseConfirm: () => void;
  onCloseCreate: () => void;
  onCloseNotice: () => void;
  onClosePicker: () => void;
  onConfirmRemove: () => void;
  onCreateFolder: (name: string) => void;
  onManage: (anchor?: HTMLElement | null) => void;
  onOpenCreate: () => void;
  onSaveFolders: (folderIDs: string[]) => void;
  onStartRemove: () => void;
  onUndo: () => void;
};

function FavoriteActionsView(props: FavoriteActionsViewProps) {
  const {
    busy, confirming, createError, creating, favorite, folders, heartButton, internalManageButton,
    membershipCount, notice, onAdd, onCloseConfirm, onCloseCreate, onCloseNotice, onClosePicker,
    onConfirmRemove, onCreateFolder, onManage, onOpenCreate, onSaveFolders, onStartRemove, onUndo,
    picker, pickerAnchor, resolvePickerReturnFocus, showManageButton, title, variant,
  } = props;
  return <>
    <div className={`favorite-actions favorite-actions-${variant}`}>
      <button
        ref={heartButton}
        className={`favorite-heart ${favorite ? "is-favorite" : ""}`}
        type="button"
        aria-label={favorite ? `取消收藏“${title}”` : `收藏“${title}”`}
        aria-pressed={Boolean(favorite)}
        title={favorite ? "取消收藏" : "收藏"}
        disabled={busy}
        onClick={favorite ? onStartRemove : onAdd}
      ><AppIcon name="heart" />{variant === "detail" ? <span>{favorite ? "已收藏" : "收藏"}</span> : null}</button>
      {showManageButton ? <button
        ref={internalManageButton}
        className="favorite-manage"
        type="button"
        disabled={busy}
        aria-label={`管理“${title}”的收藏夹`}
        aria-haspopup="dialog"
        onClick={(event) => onManage(event.currentTarget)}
      >{variant === "favorite-card" ? "•••" : variant === "card" ? "▣" : "管理收藏夹"}</button> : null}
    </div>
    <ConfirmDialog
      open={confirming}
      portalToBody
      title={`取消收藏“${title}”？`}
      description={membershipCount ? `这会同时从 ${membershipCount} 个收藏夹移除。` : "取消后将不再出现在“我的收藏”中。"}
      confirmLabel="取消收藏"
      cancelLabel="保留收藏"
      tone="danger"
      busy={busy}
      onCancel={onCloseConfirm}
      onConfirm={onConfirmRemove}
    />
    <FolderPickerDialog
      open={picker}
      title={`管理“${title}”的收藏夹`}
      folders={folders}
      selectedFolderIds={favorite?.folderIds ?? []}
      busy={busy}
      anchor={pickerAnchor}
      resolveReturnFocus={resolvePickerReturnFocus}
      onClose={onClosePicker}
      onCreate={onOpenCreate}
      onSave={onSaveFolders}
    />
    <FolderNameDialog open={creating} title="新建收藏夹" submitLabel="创建收藏夹" busy={busy} error={createError} onClose={onCloseCreate} onSubmit={onCreateFolder} />
    <FavoriteToast {...{ busy, notice, onCloseNotice, onManage, onUndo }} />
  </>;
}

function FavoriteToast({ busy, notice, onCloseNotice, onManage, onUndo }: {
  busy: boolean;
  notice: Notice | null;
  onCloseNotice: () => void;
  onManage: () => void;
  onUndo: () => void;
}) {
  if (!notice) {return null;}
  return <div className="favorite-toast" role="status" aria-live="polite">
    <span>{notice.message}</span>
    {notice.offerManage ? <button type="button" disabled={busy} onClick={() => {onCloseNotice(); onManage();}}>加入收藏夹</button> : null}
    {notice.undo?.length ? <button type="button" disabled={busy} onClick={onUndo}>撤销</button> : null}
    <button type="button" aria-label="关闭通知" onClick={onCloseNotice}>×</button>
  </div>;
}
