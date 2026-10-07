"use client";
import { useLayoutEffect, useRef, useState, type KeyboardEvent } from "react";
import { createPortal } from "react-dom";
import type { Directory } from "@/lib/api/types";

export function DirectoryMenu({
  directory,
  onEdit,
  onDelete,
}: {
  directory: Directory;
  onEdit: (directory: Directory) => void;
  onDelete: (directory: Directory) => void;
}) {
  const [open, setOpen] = useState(false);
  const trigger = useRef<HTMLButtonElement>(null);
  const panel = useRef<HTMLDivElement>(null);
  useLayoutEffect(() => {
    if (!open) {
      return;
    }
    function position() {
      if (!panel.current || !trigger.current) {
        return;
      }
      if (matchMedia("(max-width: 767px)").matches) {
        panel.current.style.removeProperty("left");
        panel.current.style.removeProperty("top");
        return;
      }
      const anchor = trigger.current.getBoundingClientRect();
      const box = panel.current.getBoundingClientRect();
      panel.current.style.left = `${Math.max(8, Math.min(anchor.right - box.width, innerWidth - box.width - 8))}px`;
      panel.current.style.top = `${Math.max(8, Math.min(anchor.bottom + 6, innerHeight - box.height - 8))}px`;
    }
    function dismiss(event: PointerEvent) {
      if (
        event.target instanceof Node &&
        !panel.current?.contains(event.target) &&
        !trigger.current?.contains(event.target)
      ) {
        setOpen(false);
      }
    }
    position();
    panel.current
      ?.querySelector<HTMLButtonElement>("button")
      ?.focus({ preventScroll: true });
    window.addEventListener("resize", position);
    window.addEventListener("scroll", position, true);
    document.addEventListener("pointerdown", dismiss);
    return () => {
      window.removeEventListener("resize", position);
      window.removeEventListener("scroll", position, true);
      document.removeEventListener("pointerdown", dismiss);
    };
  }, [open]);
  function keyDown(event: KeyboardEvent<HTMLDivElement>) {
    if (event.key === "Escape") {
      event.preventDefault();
      setOpen(false);
      trigger.current?.focus();
      return;
    }
    if (!["ArrowDown", "ArrowUp", "Home", "End"].includes(event.key)) {
      return;
    }
    event.preventDefault();
    const buttons = Array.from(
      panel.current?.querySelectorAll<HTMLButtonElement>(
        "button:not(:disabled)",
      ) ?? [],
    );
    const index = buttons.indexOf(document.activeElement as HTMLButtonElement);
    const next =
      event.key === "Home"
        ? 0
        : event.key === "End"
          ? buttons.length - 1
          : (index + (event.key === "ArrowDown" ? 1 : -1) + buttons.length) %
            buttons.length;
    buttons[next]?.focus();
  }
  return (
    <div className="platform-more-wrap">
      <button
        ref={trigger}
        className="platform-directory-more"
        type="button"
        aria-label={`管理目录“${directory.name}”`}
        aria-haspopup="menu"
        aria-expanded={open}
        onClick={() => setOpen(!open)}
      >
        •••
      </button>
      {open
        ? createPortal(
            <div
              ref={panel}
              className="platform-directory-menu"
              role="menu"
              aria-label={`管理目录“${directory.name}”`}
              onKeyDown={keyDown}
              onBlur={(event) => {
                if (
                  !event.currentTarget.contains(event.relatedTarget) &&
                  event.relatedTarget !== trigger.current
                ) {
                  setOpen(false);
                }
              }}
            >
              <button
                type="button"
                role="menuitem"
                onClick={() => {
                  setOpen(false);
                  onEdit(directory);
                }}
              >
                编辑目录
              </button>
              <button
                className="danger"
                type="button"
                role="menuitem"
                disabled={directory.gameCount > 0}
                onClick={() => {
                  setOpen(false);
                  onDelete(directory);
                }}
              >
                删除{directory.gameCount === 0 ? "空" : ""}目录
              </button>
              {directory.gameCount > 0 ? (
                <small>还有 {directory.gameCount} 款游戏，无法删除</small>
              ) : null}
            </div>,
            document.body,
          )
        : null}
    </div>
  );
}
