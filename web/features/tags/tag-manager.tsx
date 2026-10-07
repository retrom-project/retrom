"use client";
import { useState } from "react";
import { api, result } from "@/lib/api/client";
import type { Tag } from "@/lib/api/types";
import { useResource } from "@/lib/use-resource";
import { PageHeader, EmptyState } from "@/components/ui";
import { ResourceState } from "@/components/resource-state";
import { ConfirmDialog } from "@/components/confirm-dialog";
async function load() {
  return result(
    await api.GET("/api/v1/admin/tags", {
      params: { query: { offset: 0, limit: 100 } },
    }),
  );
}
export function TagManager() {
  const tags = useResource(load);
  const [editing, setEditing] = useState<Tag | "new" | null>(null);
  const [deleting, setDeleting] = useState<Tag | null>(null);
  const [name, setName] = useState("");
  const [error, setError] = useState("");
  async function save() {
    try {
      if (editing === "new") {
        result(await api.POST("/api/v1/admin/tags", { body: { name } }));
      } else if (editing) {
        result(
          await api.PATCH("/api/v1/admin/tags/{tagId}", {
            params: { path: { tagId: editing.id } },
            body: { name, version: editing.version },
          }),
        );
      }
      setEditing(null);
      tags.reload();
    } catch (failure) {
      setError(failure instanceof Error ? failure.message : "保存失败。");
    }
  }
  async function remove() {
    if (!deleting) {
      return;
    }
    const response = await api.DELETE("/api/v1/admin/tags/{tagId}", {
      params: { path: { tagId: deleting.id } },
      body: { version: deleting.version },
    });
    if (response.error) {
      setError(response.error.message);
      return;
    }
    setDeleting(null);
    tags.reload();
  }
  return (
    <>
      <PageHeader
        title="标签管理"
        description="标签由实例共享，删除后会从游戏展示与筛选中隐藏。"
        actions={
          <button
            className="button"
            onClick={() => {
              setName("");
              setEditing("new");
            }}
          >
            新建标签
          </button>
        }
      />
      <ResourceState resource={tags}>
        {(data) =>
          data.items.length ? (
            <div className="stack">
              {data.items.map((tag) => (
                <article className="workspace-row" key={tag.id}>
                  <div>
                    <h2>{tag.name}</h2>
                    <p>{tag.gameCount} 款已发布游戏</p>
                  </div>
                  <div className="workspace-actions">
                    <button
                      className="button secondary"
                      onClick={() => {
                        setName(tag.name);
                        setEditing(tag);
                      }}
                    >
                      改名
                    </button>
                    <button
                      className="button secondary"
                      onClick={() => setDeleting(tag)}
                    >
                      删除
                    </button>
                  </div>
                </article>
              ))}
            </div>
          ) : (
            <EmptyState title="暂无标签" description="新建标签以便整理游戏。" />
          )
        }
      </ResourceState>
      <ConfirmDialog
        open={editing !== null}
        title={editing === "new" ? "新建标签" : "标签名称"}
        onCancel={() => setEditing(null)}
        onConfirm={() => void save()}
      >
        <label className="field">
          名称
          <input
            value={name}
            onChange={(event) => setName(event.target.value)}
          />
        </label>
        {error ? <p role="alert">{error}</p> : null}
      </ConfirmDialog>
      <ConfirmDialog
        open={deleting !== null}
        title="删除标签"
        description="这个标签将不再显示。同名重新创建的标签需要重新关联游戏。"
        tone="danger"
        onCancel={() => setDeleting(null)}
        onConfirm={() => void remove()}
      >
        {error ? <p role="alert">{error}</p> : null}
      </ConfirmDialog>
    </>
  );
}
