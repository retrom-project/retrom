"use client";
import { useState } from "react";
import type { Schema } from "@/lib/api/types";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { BrowserTime } from "@/components/browser-time";
import { FeedbackBanner } from "@/components/ui";
export function AccountLinkDialog({
  link,
  busy,
  error,
  onClose,
  onRevoke,
}: {
  link: Schema<"AccountLink">;
  busy: boolean;
  error: string;
  onClose: () => void;
  onRevoke: () => void;
}) {
  const [copied, setCopied] = useState(false);
  const [copyError, setCopyError] = useState("");
  async function copyLink() {
    try {
      await navigator.clipboard.writeText(link.url);
      setCopied(true);
      setCopyError("");
    } catch {
      setCopyError("无法自动复制，请选择链接后手动复制。");
    }
  }
  return (
    <ConfirmDialog
      open
      title={link.kind === "invitation" ? "账号邀请链接" : "密码重置链接"}
      description="请保存完整链接；关闭后，列表只显示脱敏摘要。"
      hideCancel
      confirmLabel="关闭"
      secondaryLabel="撤销链接"
      busy={busy}
      onCancel={onClose}
      onConfirm={onClose}
      onSecondary={onRevoke}
    >
      <div className="one-time-dialog">
        <div className="one-time-body">
          <label>
            链接
            <input
              value={link.url}
              readOnly
              onFocus={(event) => event.target.select()}
            />
          </label>
          <button className="button secondary" onClick={() => void copyLink()}>
            {copied ? "已复制" : "复制链接"}
          </button>
        </div>
        <dl>
          <div>
            <dt>有效期至</dt>
            <dd>
              <BrowserTime value={link.expiresAtMs} />
            </dd>
          </div>
        </dl>
        {error || copyError ? (
          <FeedbackBanner tone="bad">{error || copyError}</FeedbackBanner>
        ) : null}
      </div>
    </ConfirmDialog>
  );
}
