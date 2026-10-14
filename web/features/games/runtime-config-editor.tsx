"use client";
import { ArcadeParentFiles } from "./arcade-parent-files";
import { ScummvmSelection } from "./scummvm-selection";
import { DOSEntrySelector } from "./dos-entry-selector";
import { RPGEntrySelector } from "./rpg-entry-selector";
import { ROMSetSelector } from "./rom-set-selector";
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
  savedValue,
  gameId,
  version,
  onUploaded,
  coreIds,
  coreId,
  onCoreChange,
  files,
  onChange,
}: {
  value: Schema<"RuntimeConfig">;
  savedValue?: Schema<"RuntimeConfig">;
  gameId: string;
  version: number;
  onUploaded: () => void;
  coreIds: string[];
  coreId: string;
  onCoreChange: (coreId: string) => void;
  files: Schema<"GameFile">[];
  onChange: (value: Schema<"RuntimeConfig">) => void;
}) {
  const catalog = useResource(loadCatalog);
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
  const fields = optionDescriptors(target?.targetOptionsSchema ?? {}).filter(field => !binding?.romSetSelection || field.key !== "machine");
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
      <div className="form-grid runtime-content-fields">
        <ContentFields gameId={gameId} value={value} files={files} onEdit={content} />{" "}
        <label className="field">
          运行核心
          <select
            value={coreId}
            onChange={(event) => onCoreChange(event.target.value)}
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
      <ROMSetField binding={binding} gameId={gameId} coreId={coreId} version={version} value={value} core={core} onUpdate={updateCore} />
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
      {binding?.dependencyFiles ? <ArcadeParentFiles
        key={`${gameId}:${coreId}:${version}`}
        gameId={gameId} version={version} coreId={coreId}
        entryFile={value.content.entryFile} files={files}
        selected={core.parentFiles ?? []}
        extensions={binding.dependencyFiles.extensions}
        configurationChanged={runtimeSelectionChanged(savedValue, value, coreId)}
        onChange={(parentFiles) => updateCore({ ...core, parentFiles })}
        onUploaded={onUploaded}
      /> : null}
    </fieldset>
  );
}
function runtimeSelectionChanged(saved: Schema<"RuntimeConfig"> | undefined, current: Schema<"RuntimeConfig">, coreId: string) {
  return !!saved && (saved.content.entryFile !== current.content.entryFile || saved.cores?.[coreId]?.romSet !== current.cores?.[coreId]?.romSet);
}
function ROMSetField({ binding, gameId, coreId, version, value, core, onUpdate }: {
  binding: Schema<"RuntimeBinding"> | undefined; gameId: string; coreId: string; version: number;
  value: Schema<"RuntimeConfig">; core: Schema<"RuntimeCoreOptions">; onUpdate: (core: Schema<"RuntimeCoreOptions">) => void;
}) {
  if (!binding?.romSetSelection) { return null; }
  return <ROMSetSelector key={`${gameId}:${coreId}:${version}:${value.content.entryFile}`}
    gameId={gameId} coreId={coreId} entryFile={value.content.entryFile ?? ""}
    value={core.romSet} datSHA256={binding.romSetSelection.datSHA256} onSelect={romSet => {
      const next = { ...core };
      if (romSet) { next.romSet = romSet; } else { delete next.romSet; }
      onUpdate(next);
    }} />;
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
  gameId,
  value,
  files,
  onEdit,
}: {
  gameId: string;
  value: Schema<"RuntimeConfig">;
  files: Schema<"GameFile">[];
  onEdit: (key: "entryFile" | "entryPath" | "engine", text: string) => void;
}) {
  return (
    <>
      {" "}
      {value.content.kind === "RPG_MAKER_PROJECT" ? <RPGEntrySelector
        key={`${gameId}:${files.map(file => `${file.id}:${file.sha256}`).join(",")}`}
        gameId={gameId} engine={value.content.engine} value={value.content.entryFile ?? ""}
        onSelect={path => onEdit("entryFile", path)}
      /> : value.content.kind !== "SCUMMVM_PROJECT" ? (
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
      {value.content.kind === "DOS_BUNDLE" ? <DOSEntrySelector
        key={`${gameId}:${value.content.entryFile}:${files.map((file) => `${file.id}:${file.sha256}`).join(",")}`}
        gameId={gameId} entryFile={value.content.entryFile ?? ""} value={value.content.entryPath ?? ""}
        onSelect={(path) => onEdit("entryPath", path)}
      /> : null}
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
