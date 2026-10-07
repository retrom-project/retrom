"use client";
import { useState } from "react";
import type { Schema } from "@/lib/api/types";
import { AppIcon } from "@/components/app-icon";
export function BiosList({
  items,
  catalog,
  onInstall,
  onRemove,
}: {
  items: Schema<"BiosRequirement">[];
  catalog: Schema<"RuntimeCatalog"> | null;
  onInstall: (key: string, file: File) => void;
  onRemove: (key: string) => void;
}) {
  const [query, setQuery] = useState("");
  const [core, setCore] = useState("");
  const [state, setState] = useState("");
  const [required, setRequired] = useState("");
  const filtered = items.filter(
    (item) =>
      (!core || item.coreIds.includes(core)) &&
      (!state || String(item.installed) === state) &&
      (!required || String(item.required) === required) &&
      `${item.name} ${item.filename} ${item.coreIds.join(" ")}`
        .toLocaleLowerCase()
        .includes(query.toLocaleLowerCase()),
  );
  const groups = [
    {
      name: "需要处理",
      items: filtered.filter((item) => item.required && !item.installed),
    },
    {
      name: "已安装与可选项",
      items: filtered.filter((item) => item.installed || !item.required),
    },
  ];
  return (
    <>
      <div className="runtime-kpis">
        <article>
          <small>BIOS目录</small>
          <strong>{items.length}</strong>
          <p>运行声明中的共享要求</p>
        </article>
        <article className="has-danger">
          <small>必需文件缺失</small>
          <strong>
            {items.filter((item) => item.required && !item.installed).length}
          </strong>
          <p>可能阻止相关游戏运行</p>
        </article>
        <article>
          <small>可选文件</small>
          <strong>{items.filter((item) => !item.required).length}</strong>
          <p>以具体运行要求为准</p>
        </article>
        <article className="has-success">
          <small>当前已安装</small>
          <strong>{items.filter((item) => item.installed).length}</strong>
          <p>运行时检查内容与身份</p>
        </article>
      </div>
      <div className="runtime-toolbar panel">
        <label className="runtime-search">
          搜索文件或核心
          <span className="search">
            <AppIcon name="search" />
            <input
              value={query}
              onChange={(event) => setQuery(event.target.value)}
              placeholder="例如 gba_bios.bin 或 mGBA"
            />
          </span>
        </label>
        <label>
          运行核心
          <select
            aria-label="运行核心"
            value={core}
            onChange={(event) => setCore(event.target.value)}
          >
            <option value="">全部核心</option>
            {catalog?.cores.map((item) => (
              <option key={item.id} value={item.id}>
                {item.name}
              </option>
            ))}
          </select>
        </label>
        <label>
          文件状态
          <select
            aria-label="文件状态"
            value={state}
            onChange={(event) => setState(event.target.value)}
          >
            <option value="">所有状态</option>
            <option value="true">已安装</option>
            <option value="false">未安装</option>
          </select>
        </label>
        <label>
          要求类型
          <select
            aria-label="要求类型"
            value={required}
            onChange={(event) => setRequired(event.target.value)}
          >
            <option value="">全部要求</option>
            <option value="true">必需</option>
            <option value="false">可选</option>
          </select>
        </label>
      </div>
      {groups.map((group) => (
        <section className="runtime-section" key={group.name}>
          <div className="runtime-section-heading">
            <h2>{group.name}</h2>
            <span>{group.items.length} 项</span>
          </div>
          <div className="runtime-list">
            {group.items.map((item) => (
              <BiosRow
                key={item.key}
                item={item}
                catalog={catalog}
                onInstall={onInstall}
                onRemove={onRemove}
              />
            ))}
          </div>
          {!group.items.length ? (
            <p className="workspace-note">暂无匹配项目。</p>
          ) : null}
        </section>
      ))}
    </>
  );
}
function BiosRow({
  item,
  catalog,
  onInstall,
  onRemove,
}: {
  item: Schema<"BiosRequirement">;
  catalog: Schema<"RuntimeCatalog"> | null;
  onInstall: (key: string, file: File) => void;
  onRemove: (key: string) => void;
}) {
  return (
    <article className="runtime-bios-row">
      <div className="runtime-bios-file">
        <span className="runtime-file-mark">BIOS</span>
        <div>
          <h3>{item.name}</h3>
          <p>
            {item.required ? "必需" : "可选"} · {item.filename}
          </p>
          {item.sha256 ? (
            <dl className="runtime-technical">
              <dt>SHA-256</dt>
              <dd title={item.sha256}>{item.sha256}</dd>
            </dl>
          ) : null}
        </div>
      </div>
      <div className="runtime-core">
        <strong>
          {item.coreIds
            .map(
              (id) => catalog?.cores.find((core) => core.id === id)?.name ?? id,
            )
            .join("、")}
        </strong>
        <small>{item.coreIds.join("、")}</small>
      </div>
      <span
        className={`status ${item.installed ? "good" : item.required ? "bad" : "neutral"}`}
      >
        {item.installed ? "已安装" : "缺少文件"}
      </span>
      <div className="runtime-usage">
        <strong>共享要求</strong>
        <small>
          {item.platformIds
            .map(
              (id) =>
                catalog?.platforms.find((platform) => platform.id === id)
                  ?.name ?? id,
            )
            .join("、")}
        </small>
      </div>
      <div className="runtime-row-actions">
        <label className={`button ${item.installed ? "secondary" : ""}`}>
          {item.installed ? "替换文件" : "选择 BIOS 文件"}
          <input
            type="file"
            hidden
            onChange={(event) => {
              const file = event.target.files?.[0];
              if (file) {
                onInstall(item.key, file);
              }
            }}
          />
        </label>
        {item.installed ? (
          <button
            className="button secondary"
            onClick={() => onRemove(item.key)}
          >
            移除
          </button>
        ) : null}
      </div>
    </article>
  );
}
