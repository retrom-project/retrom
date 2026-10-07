"use client";
import { useCallback } from "react";
import { api, result } from "@/lib/api/client";
import { useResource } from "@/lib/use-resource";
async function roots() {
  return result(await api.GET("/api/v1/admin/source-roots"));
}
export function SourcePicker({
  rootId,
  relativePath,
  onChange,
}: {
  rootId: string;
  relativePath: string;
  onChange: (rootId: string, relativePath: string) => void;
}) {
  const sourceRoots = useResource(roots);
  const loader = useCallback(
    async () =>
      rootId
        ? result(
            await api.GET("/api/v1/admin/source-roots/{rootId}/directories", {
              params: { path: { rootId }, query: { relativePath } },
            }),
          )
        : { items: [] },
    [rootId, relativePath],
  );
  const directories = useResource(loader);
  return (
    <div className="stack">
      <label className="field">
        服务器来源
        <select
          aria-label="服务器来源"
          value={rootId}
          onChange={(event) => onChange(event.target.value, "")}
        >
          <option value="">选择服务器来源</option>
          {sourceRoots.data?.items.map((root) => (
            <option value={root.id} key={root.id}>
              {root.name}
            </option>
          ))}
        </select>
      </label>
      <label className="field">
        来源内目录
        <input
          aria-label="来源内目录"
          value={relativePath}
          onChange={(event) => onChange(rootId, event.target.value)}
          placeholder="相对路径，空值表示来源根目录"
        />
      </label>
      <div className="workspace-actions">
        {relativePath ? (
          <button
            className="button secondary"
            onClick={() =>
              onChange(rootId, relativePath.split("/").slice(0, -1).join("/"))
            }
          >
            上级目录
          </button>
        ) : null}
        {directories.data?.items.map((directory) => (
          <button
            className="button secondary"
            key={directory.relativePath}
            onClick={() => onChange(rootId, directory.relativePath)}
          >
            {directory.name}
          </button>
        ))}
      </div>
      {sourceRoots.error || directories.error ? (
        <p role="alert">{sourceRoots.error || directories.error}</p>
      ) : null}
    </div>
  );
}
