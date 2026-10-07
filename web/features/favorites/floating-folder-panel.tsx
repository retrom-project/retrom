"use client";
import { useRef, useState } from "react";
import type { PointerEvent, KeyboardEvent, ReactNode } from "react";
import { AppIcon } from "@/components/app-icon";
export function FloatingFolderPanel({ children }: { children: ReactNode }) {
  const panel = useRef<HTMLElement>(null);
  const drag = useRef<{
    x: number;
    y: number;
    left: number;
    top: number;
  } | null>(null);
  const [position, setPosition] = useState<{
    left: number;
    top: number;
  } | null>(null);
  const [collapsed, setCollapsed] = useState(false);
  function move(left: number, top: number) {
    const bounds = panel.current?.getBoundingClientRect();
    if (!bounds) {
      return;
    }
    setPosition({
      left: Math.max(8, Math.min(innerWidth - bounds.width - 8, left)),
      top: Math.max(8, Math.min(innerHeight - bounds.height - 8, top)),
    });
  }
  function begin(event: PointerEvent<HTMLButtonElement>) {
    const bounds = panel.current?.getBoundingClientRect();
    if (!bounds) {
      return;
    }
    drag.current = {
      x: event.clientX,
      y: event.clientY,
      left: bounds.left,
      top: bounds.top,
    };
    event.currentTarget.setPointerCapture(event.pointerId);
  }
  function update(event: PointerEvent<HTMLButtonElement>) {
    if (drag.current) {
      move(
        drag.current.left + event.clientX - drag.current.x,
        drag.current.top + event.clientY - drag.current.y,
      );
    }
  }
  function keyboard(event: KeyboardEvent<HTMLButtonElement>) {
    const direction = {
      ArrowLeft: [-20, 0],
      ArrowRight: [20, 0],
      ArrowUp: [0, -20],
      ArrowDown: [0, 20],
    }[event.key];
    const bounds = panel.current?.getBoundingClientRect();
    if (direction && bounds) {
      event.preventDefault();
      move(bounds.left + direction[0], bounds.top + direction[1]);
    }
  }
  return (
    <aside
      ref={panel}
      className={`favorite-rail favorite-floating-navigation${collapsed ? " is-collapsed" : ""}`}
      style={position ?? undefined}
    >
      <header>
        <button
          className="favorite-navigation-drag"
          onPointerDown={begin}
          onPointerMove={update}
          onPointerUp={() => {
            drag.current = null;
          }}
          onPointerCancel={() => {
            drag.current = null;
          }}
          onKeyDown={keyboard}
          aria-label="移动收藏导航（方向键或拖动）"
        >
          <AppIcon name="menu" />
          收藏导航
        </button>
        <button
          className="favorite-navigation-collapse"
          aria-label={collapsed ? "展开收藏导航" : "收起收藏导航"}
          aria-expanded={!collapsed}
          onClick={() => setCollapsed(!collapsed)}
        >
          <AppIcon name={collapsed ? "expand" : "minimize"} />
        </button>
      </header>
      <div hidden={collapsed}>{children}</div>
    </aside>
  );
}
