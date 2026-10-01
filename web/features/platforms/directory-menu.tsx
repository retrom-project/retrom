"use client";

import { useLayoutEffect, useRef, type KeyboardEvent } from "react";
import { createPortal } from "react-dom";
import type { EditTarget } from "./platform-manager";
import type { PlatformInstance } from "./platform-directory-list";

type Props = {
  busy: string | null;
  instance: PlatformInstance;
  menuOpen: boolean;
  onDelete: (instance: PlatformInstance) => void;
  onEdit: (target: EditTarget) => void;
  onMenu: (id: string | null) => void;
};

export function DirectoryMenu({ busy, instance, menuOpen, onDelete, onEdit, onMenu }: Props) {
  const trigger = useRef<HTMLButtonElement>(null);
  const panel = useRef<HTMLDivElement>(null);
  const close = () => { onMenu(null); trigger.current?.focus(); };

  useLayoutEffect(() => {
    if (!menuOpen) {return;}
    const position = () => {
      if (!panel.current || !trigger.current) {return;}
      const anchor = trigger.current.getBoundingClientRect();
      const box = panel.current.getBoundingClientRect();
      panel.current.style.left = `${Math.max(8, Math.min(anchor.right - box.width, innerWidth - box.width - 8))}px`;
      panel.current.style.top = `${Math.max(8, Math.min(anchor.bottom + 6, innerHeight - box.height - 8))}px`;
    };
    position();
    panel.current?.querySelector<HTMLButtonElement>("button")?.focus({ preventScroll: true });
    window.addEventListener("resize", position);
    window.addEventListener("scroll", position, true);
    return () => {
      window.removeEventListener("resize", position);
      window.removeEventListener("scroll", position, true);
    };
  }, [menuOpen]);

  function keyDown(event: KeyboardEvent<HTMLDivElement>) {
    if (event.key === "Escape") {event.preventDefault(); close(); return;}
    if (!["ArrowDown", "ArrowUp", "Home", "End"].includes(event.key)) {return;}
    event.preventDefault();
    const buttons = Array.from(panel.current?.querySelectorAll<HTMLButtonElement>("button:not(:disabled)") ?? []);
    const index = buttons.indexOf(document.activeElement as HTMLButtonElement);
    const next = event.key === "Home" ? 0 : event.key === "End" ? buttons.length - 1 : (index + (event.key === "ArrowDown" ? 1 : -1) + buttons.length) % buttons.length;
    buttons[next]?.focus();
  }

  const edit = (field: "name" | "description") => { onEdit({ id: instance.id, field }); onMenu(null); };
  return <div className="platform-more-wrap" role="cell">
    <button ref={trigger} className="platform-directory-more" type="button" aria-label={`管理目录“${instance.name}”`} aria-haspopup="menu" aria-expanded={menuOpen} onClick={() => onMenu(menuOpen ? null : instance.id)}>•••</button>
    {menuOpen ? createPortal(<div ref={panel} className="platform-directory-menu" role="menu" aria-label={`管理目录“${instance.name}”`} onKeyDown={keyDown} onBlur={(event) => { if (!event.currentTarget.contains(event.relatedTarget) && event.relatedTarget !== trigger.current) {onMenu(null);} }}>
      <button type="button" role="menuitem" onClick={() => edit("name")}>编辑名称</button>
      <button type="button" role="menuitem" onClick={() => edit("description")}>编辑说明</button>
      <button className="danger" type="button" role="menuitem" disabled={instance.gameCount > 0 || busy !== null} onClick={() => { onDelete(instance); onMenu(null); }}>删除{instance.gameCount === 0 ? "空" : ""}目录</button>
      {instance.gameCount > 0 ? <small>还有 {instance.gameCount} 款游戏，无法删除</small> : null}
    </div>, document.body) : null}
  </div>;
}
