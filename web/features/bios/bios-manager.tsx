"use client";
import {
  api,
  result,
  readError,
  writeHeaders,
  ApiError,
} from "@/lib/api/client";
import { useResource } from "@/lib/use-resource";
import { PageHeader } from "@/components/ui";
import { ResourceState } from "@/components/resource-state";
import { BiosList } from "./bios-list";
import { useToast } from "@/components/toast-provider";
async function load() {
  return result(await api.GET("/api/v1/admin/bios"));
}
async function catalog() {
  return result(await api.GET("/api/v1/runtime/catalog"));
}
export function BiosManager() {
  const { notify } = useToast();
  const bios = useResource(load);
  const declarations = useResource(catalog);
  async function install(key: string, file: File) {
    const body = new FormData();
    body.set("file", file);
    try {
      const response = await fetch(
        `/api/v1/admin/bios/${encodeURIComponent(key)}`,
        {
          method: "PUT",
          headers: writeHeaders(),
          credentials: "same-origin",
          body,
        },
      );
      if (!response.ok) {
        const failure = readError(await response.json());
        throw new ApiError(failure.code, failure.message, response.status);
      }
      bios.reload();
      notify({ tone: "good", message: "BIOS 文件已安装。" });
    } catch (failure) {
      notify({
        tone: "bad",
        message: failure instanceof Error ? failure.message : "上传失败。",
      });
    }
  }
  async function remove(requirementKey: string) {
    try {
      const response = await api.DELETE("/api/v1/admin/bios/{requirementKey}", {
        params: { path: { requirementKey } },
      });
      if (response.error) {
        throw new ApiError(response.error.code, response.error.message, response.response.status);
      }
      bios.reload();
      notify({ tone: "good", message: "BIOS 文件已移除。" });
    } catch (failure) {
      notify({
        tone: "bad",
        message: failure instanceof Error ? failure.message : "移除失败。",
      });
    }
  }
  return (
    <>
      <PageHeader
        title="运行依赖"
        description="管理游戏所需的 BIOS 与运行文件，查看要求、上传替换或移除安装。"
      />
      <ResourceState resource={bios}>
        {(data) => (
          <BiosList
            items={data.items}
            catalog={declarations.data}
            onInstall={(key, file) => void install(key, file)}
            onRemove={(key) => void remove(key)}
          />
        )}
      </ResourceState>
    </>
  );
}
