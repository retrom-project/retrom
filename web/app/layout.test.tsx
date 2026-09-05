import type { ReactNode } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { afterEach, describe, expect, it, vi } from "vitest";
import RootLayout from "./layout";

vi.mock("@/features/auth/server", () => ({ loadAuthContext: async () => null }));
vi.mock("@/features/auth/auth-provider", () => ({
  AuthProvider: ({ children }: { children: ReactNode }) => <>{children}</>,
}));
vi.mock("@/components/app-shell", () => ({
  AppShell: ({ children }: { children: ReactNode }) => <main>{children}</main>,
}));

afterEach(() => { vi.unstubAllEnvs(); });

describe("document bootstrap", () => {
  it.each(["development", "production"])("does not inject browser error suppression in %s", async (mode) => {
    vi.stubEnv("NODE_ENV", mode);
    const markup = renderToStaticMarkup(await RootLayout({ children: <p>Retrom</p> }));
    const document = new DOMParser().parseFromString(markup, "text/html");

    expect(document.documentElement.lang).toBe("zh-CN");
    expect(document.querySelector("main")?.textContent).toBe("Retrom");
    // Next owns its framework bootstrap. The shared layout must not inject an
    // inline error handler, even one narrowly matching an anonymous DevTools stack.
    expect(document.querySelectorAll("script")).toHaveLength(0);
  });
});
