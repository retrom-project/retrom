"use client";
import { useState } from "react";
import { useAuth } from "@/features/auth/auth-provider";
import { PageHeader, StatusBadge } from "@/components/ui";
import { PwaSettings } from "@/features/pwa/pwa-controls";
import { BrowserStorageSettings } from "./browser-storage-settings";
import { PasswordDialog } from "./password-dialog";
export function AccountPage() {
  const { context } = useAuth();
  const [passwordOpen, setPasswordOpen] = useState(false);
  return (
    <>
      <PageHeader
        title="账户设置"
        description="查看当前账号资料并更新登录密码。"
      />
      <div className="account-settings-grid">
        <section className="panel">
          <div className="panel-head account-profile-head">
            <div>
              <h2>账号资料</h2>
              <p>本版本不提供自行修改用户名或显示名称。</p>
            </div>
            <button className="button secondary" type="button" onClick={() => setPasswordOpen(true)}>
              修改密码
            </button>
          </div>
          <dl className="account-facts">
            <div>
              <dt>用户名</dt>
              <dd>@{context?.user?.username}</dd>
            </div>
            <div>
              <dt>显示名称</dt>
              <dd>{context?.user?.displayName}</dd>
            </div>
            <div>
              <dt>角色</dt>
              <dd>
                <StatusBadge
                  tone={context?.user?.role === "admin" ? "info" : "neutral"}
                >
                  {context?.user?.role === "admin" ? "管理员" : "普通用户"}
                </StatusBadge>
              </dd>
            </div>
            <div>
              <dt>账号状态</dt>
              <dd>
                <StatusBadge tone="good">启用</StatusBadge>
              </dd>
            </div>
          </dl>
        </section>
        <PwaSettings />
      </div>
      <BrowserStorageSettings />
      {passwordOpen ? <PasswordDialog onClose={() => setPasswordOpen(false)} /> : null}
    </>
  );
}
