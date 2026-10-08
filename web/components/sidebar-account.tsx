"use client";

import Link from "next/link";
import { useEffect, useRef } from "react";
import { useAuth } from "@/features/auth/auth-provider";
import { AppIcon } from "./app-icon";
import { ServiceHealth } from "./service-health";

export function SidebarAccount() {
  const { context, logout } = useAuth();
  const menu = useRef<HTMLDetailsElement>(null);
  function close() {
    if (menu.current) {
      menu.current.open = false;
    }
  }
  useEffect(() => {
    function pointer(event: PointerEvent) {
      if (event.target instanceof Node && !menu.current?.contains(event.target)) {
        close();
      }
    }
    function keyboard(event: KeyboardEvent) {
      if (event.key === "Escape" && menu.current?.open) {
        close();
        menu.current.querySelector("summary")?.focus();
      }
    }
    document.addEventListener("pointerdown", pointer);
    document.addEventListener("keydown", keyboard);
    return () => {
      document.removeEventListener("pointerdown", pointer);
      document.removeEventListener("keydown", keyboard);
    };
  }, []);
  return (
    <div className="sidebar-account-row">
      <details ref={menu} className="account-menu">
        <summary>
          <span className="account-initial">{context?.user?.displayName.slice(0, 1)}</span>
          <span className="account-copy"><strong>{context?.user?.displayName}</strong></span>
        </summary>
        <div className="account-menu-popover">
          <Link href="/account" onClick={close}><AppIcon name="settings" />账户设置</Link>
          <button onClick={() => void logout()}><AppIcon name="log-out" />退出登录</button>
        </div>
      </details>
      <ServiceHealth />
    </div>
  );
}
