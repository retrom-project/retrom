import type { Metadata } from "next";
import { SaveFailureNotice } from "@/features/player/save-failure-notice";
import { AppShell } from "@/components/app-shell";
import { AuthProvider } from "@/features/auth/auth-provider";
import { ToastProvider } from "@/components/toast-provider";
import "./globals.css";
export const metadata: Metadata = {
  title: { default: "Retrom", template: "%s · Retrom" },
  description: "自托管复古游戏资料库与运行环境",
};
export default function RootLayout({
  children,
}: Readonly<{ children: React.ReactNode }>) {
  return (
    <html lang="zh-CN">
      <body>
        <ToastProvider>
          <AuthProvider>
            <AppShell>{children}</AppShell>
            <SaveFailureNotice />
          </AuthProvider>
        </ToastProvider>
      </body>
    </html>
  );
}
