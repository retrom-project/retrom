"use client";
import Link from "next/link";
import { useEffect, useState } from "react";
import { useAuth } from "@/features/auth/auth-provider";
export function SaveFailureNotice() {
  const { context } = useAuth();
  const userId = context?.user?.id;
  const [notice, setNotice] = useState<{
    userId: string;
    message: string;
  } | null>(null);
  useEffect(() => {
    function receive(event: Event) {
      if (!(event instanceof CustomEvent)) {
        return;
      }
      const value: unknown = event.detail;
      if (
        value &&
        typeof value === "object" &&
        "userId" in value &&
        "message" in value &&
        typeof value.userId === "string" &&
        typeof value.message === "string"
      ) {
        setNotice({ userId: value.userId, message: value.message });
      }
    }
    window.addEventListener("retrom:save-failure", receive);
    return () => window.removeEventListener("retrom:save-failure", receive);
  }, []);
  if (!notice || notice.userId !== userId) {
    return null;
  }
  return (
    <aside className="local-game-save-notice" role="alert">
      <p>{notice.message}</p>
      <Link href="/saves">检查未同步存档</Link>
      <button className="button secondary" onClick={() => setNotice(null)}>
        关闭提示
      </button>
    </aside>
  );
}
