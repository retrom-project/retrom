import { PlayerShell } from "@/features/player/player-shell";

export default async function PlayPage({
  params,
  searchParams,
}: {
  params: Promise<{ launchId: string }>;
  searchParams: Promise<{ experience?: string }>;
}) {
  const { launchId } = await params;
  const query = await searchParams;
  return <PlayerShell launchId={launchId} experience={query.experience === "immersive" ? "immersive" : "standard"} />;
}
