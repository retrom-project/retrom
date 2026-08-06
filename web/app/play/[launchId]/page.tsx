import { PlayerShell } from "@/features/player/player-shell";

export default async function PlayPage({ params }: { params: Promise<{ launchId: string }> }) {
  const { launchId } = await params;
  return <PlayerShell launchId={launchId} />;
}
