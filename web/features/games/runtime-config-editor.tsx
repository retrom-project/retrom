"use client";
import { ScummvmSelection } from "./scummvm-selection";
import { useState } from "react";
import { api, result } from "@/lib/api/client";
import type { Schema } from "@/lib/api/types";
import { useResource } from "@/lib/use-resource";
import {
  optionDescriptors,
  RuntimeOptionFields,
} from "./runtime-option-fields";
async function loadCatalog() {
  return result(await api.GET("/api/v1/runtime/catalog"));
}
export function RuntimeConfigEditor({
  value,
  gameId,
  coreIds,
  files,
  onChange,
}: {
  value: Schema<"RuntimeConfig">;
  gameId: string;
  coreIds: string[];
  files: Schema<"GameFile">[];
  onChange: (value: Schema<"RuntimeConfig">) => void;
}) {
  const catalog = useResource(loadCatalog);
  const [coreId, setCoreId] = useState(coreIds[0] ?? "");
  const binding = catalog.data?.bindings.find(
    (item) =>
      item.coreId === coreId &&
      item.contentKinds.includes(value.content.kind) &&
      (!value.content.engine || item.engine === value.content.engine),
  );
  const target = catalog.data?.providers
    .find((item) => item.providerId === binding?.providerId)
    ?.targets.find((item) => item.id === binding?.targetId);
  const core = value.cores?.[coreId] ?? {};
  const fields = optionDescriptors(target?.targetOptionsSchema ?? {});
  function content(key: "entryFile" | "entryPath" | "engine", text: string) {
    const next = { ...value.content };
    if (text) {
      if (key === "engine") {
        next.engine = text as Schema<"RuntimeContent">["engine"];
      } else {
        next[key] = text;
      }
    } else {
      delete next[key];
    }
    onChange({ ...value, content: next });
  }
  function updateCore(next: Schema<"RuntimeCoreOptions">) {
    onChange({ ...value, cores: { ...value.cores, [coreId]: next } });
  }
  return (
    <fieldset className="stack">
      <legend>运行配置</legend>
      <p className="workspace-note">{contentNames[value.content.kind]}</p>
      <div className="form-grid">
        <ContentFields value={value} files={files} onEdit={content} />{" "}
        <label className="field">
          运行核心
          <select
            value={coreId}
            onChange={(event) => setCoreId(event.target.value)}
          >
            {coreIds.map((id) => (
              <option key={id} value={id}>
                {catalog.data?.cores.find((item) => item.id === id)?.name ?? id}
              </option>
            ))}
          </select>
        </label>
      </div>
      {catalog.error ? <p role="alert">{catalog.error}</p> : null}
      {value.content.kind === "SCUMMVM_PROJECT" ? (
        <ScummvmSelection
          gameId={gameId}
          onSelect={(options) => updateCore({ ...core, options })}
        />
      ) : null}
      <RuntimeOptionFields
        fields={fields}
        options={core.options ?? {}}
        onChange={(options) => updateCore({ ...core, options })}
      />
      {binding?.contentKinds.includes("ARCADE") ? (
        <label className="field">
          Parent 文件
          <select
            multiple
            value={core.parentFiles ?? []}
            onChange={(event) =>
              updateCore({
                ...core,
                parentFiles: Array.from(
                  event.target.selectedOptions,
                  (item) => item.value,
                ),
              })
            }
          >
            {files.map((file) => (
              <option key={file.logicalKey} value={file.logicalKey}>
                {file.logicalKey}
              </option>
            ))}
          </select>
        </label>
      ) : null}
    </fieldset>
  );
}
const rpgEngines = [
  "RPG2000",
  "RPG2003",
  "RPGXP",
  "RPGVX",
  "RPGVXACE",
  "RPGMV",
  "RPGMZ",
];
const contentNames: Record<Schema<"RuntimeContent">["kind"], string> = {
  SINGLE_FILE: "单文件游戏",
  DOS_BUNDLE: "DOS 游戏项目",
  ARCADE: "街机游戏",
  RPG_MAKER_PROJECT: "RPG Maker 游戏项目",
  SCUMMVM_PROJECT: "ScummVM 游戏项目",
  ONS_PROJECT: "ONS 游戏项目",
  KIRIKIRI_PROJECT: "吉里吉里游戏项目",
  BUTTERSCOTCH_PROJECT: "Butterscotch 游戏项目",
  TYRANOSCRIPT_PROJECT: "TyranoScript 游戏项目",
  NXENGINE_PROJECT: "洞窟物语游戏项目",
  DAPHNE_PROJECT: "Daphne 游戏项目",
};

function ContentFields({
  value,
  files,
  onEdit,
}: {
  value: Schema<"RuntimeConfig">;
  files: Schema<"GameFile">[];
  onEdit: (key: "entryFile" | "entryPath" | "engine", text: string) => void;
}) {
  return (
    <>
      {" "}
      {value.content.kind !== "SCUMMVM_PROJECT" ? (
        <label className="field">
          入口文件
          <select
            value={value.content.entryFile ?? ""}
            onChange={(event) => onEdit("entryFile", event.target.value)}
          >
            <option value="">请选择入口</option>
            {files.map((file) => (
              <option key={file.logicalKey} value={file.logicalKey}>
                {file.logicalKey}
              </option>
            ))}
          </select>
        </label>
      ) : null}
      {value.content.kind === "DOS_BUNDLE" ? (
        <label className="field">
          启动程序
          <input
            value={value.content.entryPath ?? ""}
            onChange={(event) => onEdit("entryPath", event.target.value)}
            placeholder="留空使用核心启动菜单，或填写包内程序路径"
          />
        </label>
      ) : null}
      {value.content.kind === "RPG_MAKER_PROJECT" ? (
        <label className="field">
          RPG Maker 版本
          <select
            value={value.content.engine ?? ""}
            onChange={(event) => onEdit("engine", event.target.value)}
          >
            <option value="">请选择版本</option>
            {rpgEngines.map((engine) => (
              <option key={engine} value={engine}>
                {engine.replace("RPG", "RPG Maker ")}
              </option>
            ))}
          </select>
        </label>
      ) : null}
    </>
  );
}
