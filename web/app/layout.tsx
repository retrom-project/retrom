import type { Metadata } from "next";
import { AppShell } from "@/components/app-shell";
import { AuthProvider } from "@/features/auth/auth-provider";
import { loadAuthContext } from "@/features/auth/server";
import "./globals.css";

export const metadata: Metadata = {
  title: { default: "Retrom", template: "%s · Retrom" },
  description: "自托管复古游戏资料库与运行环境"
};

export const dynamic = "force-dynamic";

export default async function RootLayout({ children }: Readonly<{ children: React.ReactNode }>) {
  const initialContext = await loadAuthContext();
  return (
    <html lang="zh-CN">
      <body>
        <AuthProvider initialContext={initialContext}>
          <AppShell>{children}</AppShell>
        </AuthProvider>
      </body>
    </html>
  );
}
