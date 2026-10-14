import type { Metadata, Viewport } from "next";
import { PwaProvider } from "@/features/pwa/pwa-provider";
import { SaveFailureNotice } from "@/features/player/save-failure-notice";
import { AppShell } from "@/components/app-shell";
import { AuthProvider } from "@/features/auth/auth-provider";
import { ToastProvider } from "@/components/toast-provider";
import "./globals.css";
export const metadata: Metadata = {
  title: { default: "Retrom", template: "%s · Retrom" },
  description: "自托管复古游戏资料库与运行环境",
  applicationName: "Retrom",
  appleWebApp: { capable: true, title: "Retrom", statusBarStyle: "default" },
  icons: { apple: "/pwa/icons/apple-180.png" },
};
export const viewport: Viewport = { themeColor: "#101521", colorScheme: "dark" };
export default function RootLayout({
  children,
}: Readonly<{ children: React.ReactNode }>) {
  return (
    <html lang="zh-CN">
      <body>
        <ToastProvider>
          <PwaProvider>
            <AuthProvider>
              <AppShell>{children}</AppShell>
              <SaveFailureNotice />
            </AuthProvider>
          </PwaProvider>
        </ToastProvider>
      </body>
    </html>
  );
}
