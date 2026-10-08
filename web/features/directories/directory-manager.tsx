"use client";
import { useId, useState } from "react";
import type { FormEvent } from "react";
import { api, result, ApiError } from "@/lib/api/client";
import type { Directory, Schema } from "@/lib/api/types";
import { useResource } from "@/lib/use-resource";
import { PageHeader, EmptyState } from "@/components/ui";
import { ResourceState } from "@/components/resource-state";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { DirectoryList } from "./directory-list";
import { ResponsiveSheet } from "@/components/responsive-sheet";
import { AppIcon } from "@/components/app-icon";
import styles from "./directory.module.css";
import { useToast } from "@/components/toast-provider";
import { RecommendedDirectoriesButton } from "./recommended-directories-button";
async function load() {
  return result(await api.GET("/api/v1/admin/platform-instances"));
}
async function catalog() {
  return result(await api.GET("/api/v1/runtime/catalog"));
}
export function DirectoryManager() {
  const { notify } = useToast();
  const directories = useResource(load);
  const declarations = useResource(catalog);
  const [editing, setEditing] = useState<Directory | "new" | null>(null);
  const [deleting, setDeleting] = useState<Directory | null>(null);
  const [busy, setBusy] = useState(false);
  async function remove() {
    if (!deleting) {
      return;
    }
    setBusy(true);
    try {
      const response = await api.DELETE(
        "/api/v1/admin/platform-instances/{directoryId}",
        {
          params: { path: { directoryId: deleting.id } },
          body: { version: deleting.version },
        },
      );
      if (response.error) {
        throw new ApiError(response.error.code, response.error.message, response.response.status);
      }
      setDeleting(null);
      directories.reload();
      notify({ tone: "good", message: "游戏目录已删除。" });
    } catch (failure) {
      notify({
        tone: "bad",
        message: failure instanceof Error ? failure.message : "删除失败。",
      });
    } finally {
      setBusy(false);
    }
  }
  return (
    <div className="platform-directory-manager">
      <PageHeader
        title="游戏目录"
        description="维护游戏集合及其推荐运行方式。"
        actions={
          <><RecommendedDirectoriesButton catalog={declarations.data} onCreated={directories.reload} /><button className="button" onClick={() => setEditing("new")}>
            <AppIcon name="plus" />
            新建游戏目录
          </button></>
        }
      />
      <ResourceState resource={directories}>
        {(data) =>
          data.items.length ? (
            <DirectoryList
              directories={data.items}
              catalog={declarations.data}
              onEdit={setEditing}
              onDelete={setDeleting}
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
        busy={busy}
        onCancel={() => setDeleting(null)}
        onConfirm={() => void remove()}
      />
    </div>
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
  const { notify } = useToast();
  const formId = useId();
  const initial = initialValues(directory, catalog);
  const [platform, setPlatform] = useState(initial.platform);
  const [cores, setCores] = useState(initial.cores);
  const [defaultCore, setDefaultCore] = useState(initial.defaultCore);
  const [name, setName] = useState(initial.name);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const available = catalog.cores.filter((core) =>
    core.platformIds.includes(platform),
  );
  async function save(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const form = new FormData(event.currentTarget);
    const body: Schema<"DirectoryWriteRequest"> = {
      platformId: platform,
      name,
      slug: String(form.get("slug")),
      description: String(form.get("description")),
      coreIds: cores,
      defaultCoreId: defaultCore,
      enabled: form.has("enabled"),
    };
    setBusy(true);
    setError("");
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
      notify({
        tone: "good",
        message: directory ? "游戏目录已更新。" : "游戏目录已创建。",
      });
    } catch (failure) {
      const message = failure instanceof Error ? failure.message : "保存失败。";
      if (failure instanceof ApiError && failure.status === 400) {
        setError(message);
      } else {
        notify({ tone: "bad", message });
      }
    } finally {
      setBusy(false);
    }
  }
  return (
    <ResponsiveSheet
      open
      busy={busy}
      title={directory ? "编辑游戏目录" : "新建游戏目录"}
      description="选择游戏平台、目录信息及推荐运行方式。"
      placement="right"
      className={styles.drawer}
      onClose={onClose}
      footer={
        <>
          <button
            className="button secondary"
            disabled={busy}
            onClick={onClose}
          >
            取消
          </button>
          <button
            className="button"
            form={formId}
            type="submit"
            disabled={busy || !cores.length || !cores.includes(defaultCore)}
          >
            {busy ? "正在保存…" : directory ? "保存更改" : "创建目录"}
          </button>
        </>
      }
    >
      <form id={formId} onSubmit={(event) => void save(event)}>
        <section className="platform-drawer-step">
          <span>1</span>
          <div>
            <h3>选择游戏平台</h3>
            <label>
              游戏平台
              <select
                aria-label="游戏平台"
                value={platform}
                onChange={(event) => {
                  const next = event.target.value;
                  const core =
                    catalog.cores.find((item) =>
                      item.platformIds.includes(next),
                    )?.id ?? "";
                  setPlatform(next);
                  setCores(core ? [core] : []);
                  setDefaultCore(core);
                }}
              >
                {catalog.platforms.map((item) => (
                  <option key={item.id} value={item.id}>
                    {item.name}
                  </option>
                ))}
              </select>
            </label>
          </div>
        </section>
        <DirectoryInformation
          directory={directory}
          name={name}
          onName={setName}
        />
        <section className="platform-drawer-step">
          <span>3</span>
          <div>
            <h3>选择推荐运行方式</h3>
            <label>
              默认核心
              <select
                aria-label="默认核心"
                value={defaultCore}
                onChange={(event) => {
                  const id = event.target.value;
                  setDefaultCore(id);
                  setCores((value) =>
                    value.includes(id) ? value : [...value, id],
                  );
                }}
              >
                <option value="">选择默认核心</option>
                {available.map((core) => (
                  <option key={core.id} value={core.id}>
                    {core.name}
                  </option>
                ))}
              </select>
            </label>
            <details className={styles.cores}>
              <summary>可使用核心（{cores.length}）</summary>
              <div>
                {available.map((core) => (
                  <label key={core.id}>
                    <input
                      type="checkbox"
                      checked={cores.includes(core.id)}
                      disabled={core.id === defaultCore}
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
            </details>
            <label className={styles.enabled}>
              <input
                name="enabled"
                type="checkbox"
                defaultChecked={directory?.enabled ?? true}
              />
              启用目录
            </label>
          </div>
        </section>
        <DirectoryPreview
          directory={directory}
          catalog={catalog}
          platform={platform}
          name={name}
          defaultCore={defaultCore}
          coreCount={cores.length}
        />
        {error ? <p role="alert">{error}</p> : null}
      </form>
    </ResponsiveSheet>
  );
}

function initialValues(
  directory: Directory | null,
  catalog: Schema<"RuntimeCatalog">,
) {
  const platform = directory?.platformId ?? catalog.platforms[0]?.id ?? "";
  const defaultCore =
    directory?.defaultCoreId ??
    catalog.cores.find((core) => core.platformIds.includes(platform))?.id ??
    "";
  return {
    platform,
    defaultCore,
    cores: directory?.coreIds ?? (defaultCore ? [defaultCore] : []),
    name: directory?.name ?? "",
  };
}
function DirectoryInformation({
  directory,
  name,
  onName,
}: {
  directory: Directory | null;
  name: string;
  onName: (value: string) => void;
}) {
  return (
    <section className="platform-drawer-step">
      <span>2</span>
      <div>
        <h3>定义目录信息</h3>
        <label>
          目录名称
          <input
            aria-label="目录名称"
            name="name"
            value={name}
            onChange={(event) => onName(event.target.value)}
            placeholder="例如：我的 GBA 游戏"
            required
          />
        </label>
        <label>
          目录标识
          <input
            aria-label="目录标识"
            name="slug"
            defaultValue={directory?.slug}
            placeholder="例如：my-gba-games"
            required
          />
        </label>
        <label>
          给用户看的说明
          <textarea
            aria-label="给用户看的说明"
            name="description"
            defaultValue={directory?.description}
            placeholder="说明这个目录收录了哪些游戏（可不填）"
          />
        </label>
      </div>
    </section>
  );
}
function DirectoryPreview({
  directory,
  catalog,
  platform,
  name,
  defaultCore,
  coreCount,
}: {
  directory: Directory | null;
  catalog: Schema<"RuntimeCatalog">;
  platform: string;
  name: string;
  defaultCore: string;
  coreCount: number;
}) {
  return (
    <div className="platform-drawer-preview">
      <small>{directory ? "目录预览" : "创建预览"}</small>
      <strong>
        {catalog.platforms.find((item) => item.id === platform)?.name} ·{" "}
        {name || "未命名目录"}
      </strong>
      <ul>
        <li>
          默认使用{" "}
          {catalog.cores.find((core) => core.id === defaultCore)?.name ||
            "未选择核心"}{" "}
          启动
        </li>
        <li>{coreCount} 个可使用核心</li>
      </ul>
    </div>
  );
}
