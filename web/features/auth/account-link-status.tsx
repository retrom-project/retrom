"use client";
import { useCallback, useSyncExternalStore } from "react";
import { api, result } from "@/lib/api/client";
import { useResource } from "@/lib/use-resource";
import { BrowserTime } from "@/components/browser-time";
const subscribe = () => () => undefined;
export function useAccountLinkToken() {
  return useSyncExternalStore(
    subscribe,
    () => new URLSearchParams(location.hash.slice(1)).get("token") ?? "",
    () => "",
  );
}
export function AccountLinkStatus({ token }: { token: string }) {
  const loader = useCallback(
    async () =>
      token
        ? result(
            await api.POST("/api/v1/auth/account-links/inspect", {
              body: { token },
            }),
          )
        : null,
    [token],
  );
  const link = useResource(loader);
  if (!token) {
    return <p role="alert">链接不完整，请重新打开管理员提供的链接。</p>;
  }
  if (link.error) {
    return <p role="alert">{link.error}</p>;
  }
  return (
    <p>
      {link.data ? (
        <>
          此链接用于{link.data.kind === "invitation" ? "创建账号" : "重置密码"}
          ，有效期至 <BrowserTime value={link.data.expiresAtMs} />
        </>
      ) : (
        "正在确认链接…"
      )}
    </p>
  );
}
