"use client";
import { useCallback } from "react";
import { api, result } from "@/lib/api/client";
import { useResource } from "@/lib/use-resource";
import { AppIcon } from "@/components/app-icon";
import styles from "./scan.module.css";
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
    <div className={styles.picker}>
      <label className="field">
        <span className="field-label">服务器来源</span>
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
        <span className="field-label">来源内目录</span>
        <input
          aria-label="来源内目录"
          value={relativePath}
          onChange={(event) => onChange(rootId, event.target.value)}
          placeholder="相对路径，空值表示来源根目录"
        />
      </label>
      {rootId ? (
        <section
          className={styles.directoryBrowser}
          aria-label="服务器目录浏览器"
        >
          <header>{relativePath || "根目录"}</header>
          <div className={styles.directoryRows}>
            {relativePath ? (
              <button
                className="button ghost"
                onClick={() =>
                  onChange(
                    rootId,
                    relativePath.split("/").slice(0, -1).join("/"),
                  )
                }
              >
                <AppIcon name="arrow-left" />
                <span>上级目录</span>
              </button>
            ) : null}
            {directories.loading ? (
              <p role="status">正在读取目录…</p>
            ) : (
              directories.data?.items.map((directory) => (
                <button
                  className="button ghost"
                  key={directory.relativePath}
                  onClick={() => onChange(rootId, directory.relativePath)}
                >
                  <AppIcon name="folder" />
                  <span>{directory.name}</span>
                </button>
              ))
            )}
            {!directories.loading &&
            !directories.error &&
            !directories.data?.items.length ? (
              <p>这个目录中没有子目录。</p>
            ) : null}
          </div>
        </section>
      ) : null}
      {sourceRoots.error || directories.error ? (
        <p role="alert">{sourceRoots.error || directories.error}</p>
      ) : null}
    </div>
  );
}
