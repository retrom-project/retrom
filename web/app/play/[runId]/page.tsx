import { PlayerShell } from "@/features/player/player-shell";
export default async function Page({
  params,
  searchParams,
}: {
  params: Promise<{ runId: string }>;
  searchParams: Promise<{ returnTo?: string; contentLoading?: string }>;
}) {
  const { runId } = await params;
  const query = await searchParams;
  return (
    <PlayerShell
      runId={runId}
      returnTo={query.returnTo ?? "/library"}
      contentLoading={
        query.contentLoading === "PRELOAD" ? "PRELOAD" : "ON_DEMAND"
      }
    />
  );
}
