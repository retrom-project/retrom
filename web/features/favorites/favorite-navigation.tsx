"use client";

import { useId, useState } from "react";
import { AppIcon } from "@/components/app-icon";
import type { FavoritePage } from "./favorite-api";
import type { FavoriteQuery } from "./favorite-state";
import { useFloatingNavigation } from "./use-floating-navigation";

export function FavoriteNavigation({ onChooseScope, onCreate, onEdit, page, query }: {
  onChooseScope: (scope: FavoriteQuery["scope"], folderId?: string) => void;
  onCreate: () => void;
  onEdit?: () => void;
  page: FavoritePage | null;
  query: FavoriteQuery;
}) {
  const [collapsed, setCollapsed] = useState(false);
  const contentId = useId();
  const { anchor, panel, handle } = useFloatingNavigation(collapsed);
  return <div className="favorite-navigation-anchor" ref={anchor}>
    <aside className={`favorite-rail favorite-floating-navigation${collapsed ? " is-collapsed" : ""}`} aria-label="收藏导航" ref={panel}>
    <header>
      <button type="button" className="favorite-navigation-drag" aria-label="移动收藏导航" title="拖动移动；也可用方向键移动" {...handle}><AppIcon name="grip" /><span>收藏导航</span></button>
      <button type="button" className="favorite-navigation-collapse" aria-label={collapsed ? "展开收藏导航" : "折叠收藏导航"} aria-expanded={!collapsed} aria-controls={contentId} onClick={() => setCollapsed((value) => !value)}><AppIcon name={collapsed ? "plus" : "minimize"} /></button>
    </header>
    <div className="favorite-navigation-body" id={contentId} hidden={collapsed}>
    <nav>
      <button className={query.scope === "ALL" ? "is-active" : ""} aria-current={query.scope === "ALL" ? "page" : undefined} onClick={() => onChooseScope("ALL")}><span aria-hidden="true">♥</span><span>全部收藏</span><strong>{page?.summary.favoriteCount ?? 0}</strong></button>
      <button className={query.scope === "UNCATEGORIZED" ? "is-active" : ""} aria-current={query.scope === "UNCATEGORIZED" ? "page" : undefined} onClick={() => onChooseScope("UNCATEGORIZED")}><span aria-hidden="true">○</span><span>未分类</span><strong>{page?.summary.uncategorizedCount ?? 0}</strong></button>
      <p className="favorite-rail-label">收藏夹</p>
      {page?.folders.map((folder) => <button className={query.folderId === folder.folderId ? "is-active" : ""} aria-current={query.folderId === folder.folderId ? "page" : undefined} onClick={() => onChooseScope("FOLDER", folder.folderId)} key={folder.folderId}><span aria-hidden="true">▣</span><span>{folder.name}</span><strong>{folder.visibleGameCount}</strong></button>)}
    </nav>
    {onEdit ? <button className="favorite-edit-folder" type="button" onClick={onEdit}>编辑收藏夹</button> : null}
    <button className="favorite-new-folder" type="button" aria-label="新建收藏夹" onClick={onCreate}>＋ 新建收藏夹</button>
    </div>
  </aside></div>;
}
