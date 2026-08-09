import { DATManager, type CoreArtifact, type DATVersion } from "@/features/bios/dat-manager";
import { scalarSearchParams, type ListResponse } from "@/lib/backend";
import { backendJSON } from "@/lib/server-backend";

export const metadata = { title: "街机数据目录" };

export default async function DATPage({ searchParams }: { searchParams: Promise<Record<string, string | string[] | undefined>> }) {
  const values = scalarSearchParams(await searchParams, ["q", "source", "parseStatus"]);
  const [versions, artifacts] = await Promise.all([
    backendJSON<ListResponse<DATVersion>>("/api/v1/admin/arcade-dats"),
    backendJSON<ListResponse<CoreArtifact>>("/api/v1/admin/core-artifacts"),
  ]);
  return <DATManager versions={versions.items} artifacts={artifacts.items} initialFilters={{ query: values.q ?? "", source: values.source ?? "", parseStatus: values.parseStatus ?? "" }} />;
}
