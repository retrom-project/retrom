"use client";
import { useState } from "react";
import { api, result, readError, writeHeaders } from "@/lib/api/client";
import { useResource } from "@/lib/use-resource";
import { PageHeader } from "@/components/ui";
import { ResourceState } from "@/components/resource-state";
import { SourcePicker } from "@/features/scans/source-picker";
import { ScanProgressList } from "@/features/scans/scan-progress";
import { BiosList } from "./bios-list";
import { ResponsiveSheet } from "@/components/responsive-sheet";
async function load() {
  return result(await api.GET("/api/v1/admin/bios"));
}
async function catalog() {
  return result(await api.GET("/api/v1/runtime/catalog"));
}
export function BiosManager() {
  const bios = useResource(load);
  const declarations = useResource(catalog);
  const [rootId, setRoot] = useState("");
  const [path, setPath] = useState("");
  const [platformIds, setPlatforms] = useState<string[]>([]);
  const [coreIds, setCores] = useState<string[]>([]);
  const [error, setError] = useState("");
  const [message, setMessage] = useState("");
  const [scanning, setScanning] = useState(false);
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
        throw new Error(readError(await response.json()).message);
      }
      bios.reload();
    } catch (failure) {
      setError(failure instanceof Error ? failure.message : "上传失败。");
    }
  }
  async function remove(requirementKey: string) {
    const response = await api.DELETE("/api/v1/admin/bios/{requirementKey}", {
      params: { path: { requirementKey } },
    });
    if (response.error) {
      setError(response.error.message);
      return;
    }
    bios.reload();
  }
  async function scan() {
    try {
      result(
        await api.POST("/api/v1/admin/bios-scans", {
          body: { rootId, relativePath: path, platformIds, coreIds },
        }),
      );
      setMessage("扫描已开始。仅补齐明确匹配的缺失要求，已有安装保持原样。");
    } catch (failure) {
      setError(failure instanceof Error ? failure.message : "扫描失败。");
    }
  }
  return (
    <>
      <PageHeader
        title="BIOS 管理"
        description="管理当前安装文件，或从服务器目录补齐缺失要求。"
        actions={
          <button
            className="button secondary"
            onClick={() => setScanning(true)}
          >
            服务器扫描补齐 BIOS
          </button>
        }
      />
      {error ? <p role="alert">{error}</p> : null}
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
      <ResponsiveSheet
        open={scanning}
        title="服务端扫描补齐 BIOS"
        placement="right"
        onClose={() => setScanning(false)}
      >
        <section className="stack">
          <h2>服务端扫描补齐</h2>
          <p className="workspace-note">
            已安装项跳过，仅补齐明确匹配的缺失要求。未匹配或存在歧义时，请手动上传。
          </p>
          <SourcePicker
            rootId={rootId}
            relativePath={path}
            onChange={(root, relative) => {
              setRoot(root);
              setPath(relative);
            }}
          />
          <fieldset>
            <legend>平台范围</legend>
            <div className="workspace-tags">
              {declarations.data?.platforms.map((platform) => (
                <label key={platform.id}>
                  <input
                    type="checkbox"
                    checked={platformIds.includes(platform.id)}
                    onChange={(event) =>
                      setPlatforms((value) =>
                        event.target.checked
                          ? [...value, platform.id]
                          : value.filter((id) => id !== platform.id),
                      )
                    }
                  />
                  {platform.name}
                </label>
              ))}
            </div>
          </fieldset>
          <fieldset>
            <legend>核心范围</legend>
            <div className="workspace-tags">
              {declarations.data?.cores.map((core) => (
                <label key={core.id}>
                  <input
                    type="checkbox"
                    checked={coreIds.includes(core.id)}
                    onChange={(event) =>
                      setCores((value) =>
                        event.target.checked
                          ? [...value, core.id]
                          : value.filter((id) => id !== core.id),
                      )
                    }
                  />
                  {core.name}
                </label>
              ))}
            </div>
          </fieldset>
          <button
            className="button"
            disabled={!rootId || (!platformIds.length && !coreIds.length)}
            onClick={() => void scan()}
          >
            开始扫描
          </button>
          {message ? <p role="status">{message}</p> : null}
        </section>
      </ResponsiveSheet>
      <ScanProgressList />
    </>
  );
}
