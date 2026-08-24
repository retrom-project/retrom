import { PlatformView } from "@/features/immersive/platform-view";

export const metadata = { title: "沉浸模式" };

export default async function ImmersivePage({ searchParams }: {
  searchParams: Promise<{ platformId?: string | string[] }>;
}) {
  const query = await searchParams;
  const initialPlatformId = Array.isArray(query.platformId) ? query.platformId[0] : query.platformId;
  return <PlatformView initialPlatformId={initialPlatformId} />;
}
