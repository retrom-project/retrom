import { randomUUID } from "node:crypto";
import { expect, type Page } from "@playwright/test";

export async function deleteVideoFixture(page: Page, title: string, csrfToken: string) {
  const response = await page.request.get(`/api/v1/admin/games?q=${encodeURIComponent(title)}`);
  const games = await response.json() as { items: Array<{ gameId: string; title: string; status: string }> };
  for (const game of games.items.filter((entry) => entry.title === title && entry.status !== "DELETED")) {
    const detail = await page.request.get(`/api/v1/admin/games/${game.gameId}`);
    const data = await detail.json() as { deleteImpact: { impactDigest: string } };
    const deletion = await page.request.delete(`/api/v1/admin/games/${game.gameId}`, {
      headers: { Origin: process.env.RETROM_WEB_ORIGIN ?? "http://localhost:4000", "X-Retrom-Csrf": csrfToken, "If-Match": detail.headers().etag, "Idempotency-Key": randomUUID() },
      data: { confirmTitle: title, impactDigest: data.deleteImpact.impactDigest },
    });
    expect(deletion.ok()).toBe(true);
  }
}
