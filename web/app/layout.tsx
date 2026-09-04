import type { Metadata } from "next";
import { headers } from "next/headers";
import { AppShell } from "@/components/app-shell";
import { AuthProvider } from "@/features/auth/auth-provider";
import { loadAuthContext } from "@/features/auth/server";
import { chromeDevToolsWebVitalsGuardSource } from "@/lib/chrome-devtools-web-vitals-guard";
import "./globals.css";

export const metadata: Metadata = {
  title: { default: "Retrom", template: "%s · Retrom" },
  description: "自托管复古游戏资料库与运行环境"
};

export const dynamic = "force-dynamic";

export default async function RootLayout({ children }: Readonly<{ children: React.ReactNode }>) {
  const [initialContext, requestHeaders] = await Promise.all([loadAuthContext(), headers()]);
  const nonce = requestHeaders.get("x-nonce") ?? undefined;
  return (
    <html lang="zh-CN">
      <head>
        {process.env.NODE_ENV === "development" ? <script
          nonce={nonce}
          dangerouslySetInnerHTML={{ __html: chromeDevToolsWebVitalsGuardSource }}
        /> : null}
      </head>
      <body>
        <AuthProvider initialContext={initialContext}>
          <AppShell>{children}</AppShell>
        </AuthProvider>
      </body>
    </html>
  );
}
