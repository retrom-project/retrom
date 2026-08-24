import { PlatformView } from "@/features/immersive/platform-view";

export const metadata = { title: "沉浸模式" };

export default async function ImmersivePage({ searchParams }: {
  searchParams: Promise<{ destinationId?: string | string[]; platformId?: string | string[] }>;
}) {
  const query = await searchParams;
  const raw = query.destinationId ?? query.platformId;
  const initialDestinationId = Array.isArray(raw) ? raw[0] : raw;
  return <PlatformView initialDestinationId={initialDestinationId} />;
}
