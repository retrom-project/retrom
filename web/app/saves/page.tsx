import { SaveManager } from "@/features/saves/save-manager";
export default async function Page({
  searchParams,
}: {
  searchParams: Promise<{ gameId?: string }>;
}) {
  return <SaveManager gameId={(await searchParams).gameId} />;
}
