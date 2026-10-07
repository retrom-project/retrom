"use client";
import { useState } from "react";
import type { FormEvent } from "react";
import { api, result } from "@/lib/api/client";
import type { Directory, Schema } from "@/lib/api/types";
import { useResource } from "@/lib/use-resource";
import { PageHeader, EmptyState } from "@/components/ui";
import { ResourceState } from "@/components/resource-state";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { DirectoryList } from "./directory-list";
async function load() {
  return result(await api.GET("/api/v1/admin/platform-instances"));
}
async function catalog() {
  return result(await api.GET("/api/v1/runtime/catalog"));
}
export function DirectoryManager() {
  const directories = useResource(load);
  const declarations = useResource(catalog);
  const [editing, setEditing] = useState<Directory | "new" | null>(null);
  const [deleting, setDeleting] = useState<Directory | null>(null);
  const [error, setError] = useState("");
  async function remove() {
    if (!deleting) {
      return;
    }
    const response = await api.DELETE(
      "/api/v1/admin/platform-instances/{directoryId}",
      {
        params: { path: { directoryId: deleting.id } },
        body: { version: deleting.version },
      },
    );
    if (response.error) {
      setError(response.error.message);
      return;
    }
    setDeleting(null);
    directories.reload();
  }
  return (
    <>
      <PageHeader
        title="游戏目录"
        description="从运行声明选择平台与核心，创建属于你的游戏目录。"
        actions={
          <button className="button" onClick={() => setEditing("new")}>
            创建目录
          </button>
        }
      />
      <ResourceState resource={directories}>
        {(data) =>
          data.items.length ? (
            <DirectoryList
              directories={data.items}
              catalog={declarations.data}
              onEdit={setEditing}
              onDelete={(directory) => {
                setError("");
                setDeleting(directory);
              }}
            />
          ) : (
            <EmptyState
              title="还没有游戏目录"
              description="选择运行声明中的平台和核心，创建第一个目录。"
            />
          )
        }
      </ResourceState>
      {editing && declarations.data ? (
        <DirectoryForm
          key={editing === "new" ? "new" : editing.id}
          directory={editing === "new" ? null : editing}
          catalog={declarations.data}
          onClose={() => setEditing(null)}
          onSaved={() => {
            setEditing(null);
            directories.reload();
          }}
        />
      ) : null}
      {declarations.error ? <p role="alert">{declarations.error}</p> : null}
      <ConfirmDialog
        open={deleting !== null}
        title="删除游戏目录"
        description="目录内仍有游戏时，无法删除。"
        tone="danger"
        onCancel={() => setDeleting(null)}
        onConfirm={() => void remove()}
      >
        {error ? <p role="alert">{error}</p> : null}
      </ConfirmDialog>
    </>
  );
}
function DirectoryForm({
  directory,
  catalog,
  onClose,
  onSaved,
}: {
  directory: Directory | null;
  catalog: Schema<"RuntimeCatalog">;
  onClose: () => void;
  onSaved: () => void;
}) {
  const [platform, setPlatform] = useState(initialPlatform(directory, catalog));
  const [cores, setCores] = useState(directory?.coreIds ?? []);
  const [defaultCore, setDefaultCore] = useState(initialDefaultCore(directory));
  const [error, setError] = useState("");
  async function save(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const form = new FormData(event.currentTarget);
    const body: Schema<"DirectoryWriteRequest"> = {
      platformId: platform,
      name: String(form.get("name")),
      slug: String(form.get("slug")),
      description: String(form.get("description")),
      coreIds: cores,
      defaultCoreId: defaultCore,
      enabled: form.has("enabled"),
    };
    try {
      if (directory) {
        result(
          await api.PATCH("/api/v1/admin/platform-instances/{directoryId}", {
            params: { path: { directoryId: directory.id } },
            body: { ...body, version: directory.version },
          }),
        );
      } else {
        result(await api.POST("/api/v1/admin/platform-instances", { body }));
      }
      onSaved();
    } catch (failure) {
      setError(failure instanceof Error ? failure.message : "保存失败。");
    }
  }
  return (
    <ConfirmDialog
      open
      title={directory ? "编辑目录" : "创建目录"}
      hideCancel
      confirmLabel="关闭"
      onCancel={onClose}
      onConfirm={onClose}
    >
      <form className="stack" onSubmit={(event) => void save(event)}>
        <label className="field">
          名称
          <input name="name" defaultValue={directory?.name} required />
        </label>
        <label className="field">
          标识
          <input name="slug" defaultValue={directory?.slug} required />
        </label>
        <label className="field">
          平台
          <select
            value={platform}
            onChange={(event) => {
              setPlatform(event.target.value);
              setCores([]);
              setDefaultCore("");
            }}
          >
            {catalog.platforms.map((item) => (
              <option key={item.id} value={item.id}>
                {item.name}
              </option>
            ))}
          </select>
        </label>
        <fieldset>
          <legend>可使用核心</legend>
          {catalog.cores
            .filter((core) => core.platformIds.includes(platform))
            .map((core) => (
              <label className="field" key={core.id}>
                <span>
                  <input
                    type="checkbox"
                    checked={cores.includes(core.id)}
                    onChange={(event) =>
                      setCores((value) =>
                        event.target.checked
                          ? [...value, core.id]
                          : value.filter((id) => id !== core.id),
                      )
                    }
                  />
                  {core.name}
                </span>
              </label>
            ))}
        </fieldset>
        <label className="field">
          默认核心
          <select
            value={defaultCore}
            onChange={(event) => setDefaultCore(event.target.value)}
          >
            <option value="">选择默认核心</option>
            {cores.map((id) => (
              <option key={id} value={id}>
                {catalog.cores.find((core) => core.id === id)?.name ?? id}
              </option>
            ))}
          </select>
        </label>
        <label className="field">
          描述
          <textarea name="description" defaultValue={directory?.description} />
        </label>
        <label>
          <input
            name="enabled"
            type="checkbox"
            defaultChecked={directory?.enabled ?? true}
          />
          启用目录
        </label>
        {error ? <p role="alert">{error}</p> : null}
        <button
          className="button"
          disabled={!cores.length || !cores.includes(defaultCore)}
        >
          保存目录
        </button>
      </form>
    </ConfirmDialog>
  );
}

function initialPlatform(
  directory: Directory | null,
  catalog: Schema<"RuntimeCatalog">,
) {
  return directory?.platformId ?? catalog.platforms[0]?.id ?? "";
}
function initialDefaultCore(directory: Directory | null) {
  return directory?.defaultCoreId ?? "";
}
