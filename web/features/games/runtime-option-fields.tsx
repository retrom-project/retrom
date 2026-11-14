import type { Schema } from "@/lib/api/types";
import {useState} from "react";
type Value = Schema<"RuntimeOptionValue">;
export type OptionDescriptor = {
  key: string;
  label: string;
  type: "string" | "integer" | "boolean" | "string-array";
  required: boolean;
  choices: string[];
  minimum?: number;
  maximum?: number;
};
function record(value: Value | undefined): value is { [key: string]: Value } {
  return !!value && typeof value === "object" && !Array.isArray(value);
}
export function optionDescriptors(schema: {
  [key: string]: Value;
}): OptionDescriptor[] {
  if (!record(schema.properties)) {
    return [];
  }
  const required = Array.isArray(schema.required) ? schema.required : [];
  return Object.entries(schema.properties).flatMap(([key, value]) => {
    const stringArray = record(value) && value.type === "array" && record(value.items) && value.items.type === "string";
    if (
      !record(value) ||
      !stringArray && !["string", "integer", "boolean"].includes(String(value.type))
    ) {
      return [];
    }
    return [
      {
        key,
        label:
          typeof value.title === "string"
            ? value.title
            : (optionLabels[key] ?? key),
        type: stringArray ? "string-array" : value.type as OptionDescriptor["type"],
        required: required.includes(key),
        choices: Array.isArray(value.enum)
          ? value.enum.filter(
              (item): item is string => typeof item === "string",
            )
          : [],
        minimum: typeof value.minimum === "number" ? value.minimum : undefined,
        maximum: typeof value.maximum === "number" ? value.maximum : undefined,
      },
    ];
  });
}
export function RuntimeOptionFields({
  fields,
  options,
  onChange,
}: {
  fields: OptionDescriptor[];
  options: { [key: string]: Value };
  onChange: (options: { [key: string]: Value }) => void;
}) {
  function set(field: OptionDescriptor, text: string | boolean) {
    const next = { ...options };
    if (text === "") {
      delete next[field.key];
    } else {
      next[field.key] = field.type === "string-array" ? String(text).split(/\r?\n/u).filter(item => item !== "")
        : field.type === "integer" ? Number(text) : text;
    }
    onChange(next);
  }
  return (
    <div className="form-grid">
      {fields.map((field) => (
        <OptionField
          key={field.key}
          field={field}
          value={options[field.key]}
          onChange={(value) => set(field, value)}
        />
      ))}
    </div>
  );
}
function OptionField({
  field,
  value,
  onChange,
}: {
  field: OptionDescriptor;
  value: Value | undefined;
  onChange: (value: string | boolean) => void;
}) {
  if (field.type === "boolean") {
    return (
      <label className="field">
        <span>
          <input
            type="checkbox"
            checked={value === true}
            onChange={(event) => onChange(event.target.checked)}
          />
          {field.label}
        </span>
      </label>
    );
  }
  if (field.type === "string-array") {
    return <StringArrayField field={field} value={value} onChange={onChange} />;
  }
  const text = value === undefined || value === null ? "" : String(value);
  return (
    <label className="field">
      {field.label}
      {field.choices.length ? (
        <select
          value={text}
          required={field.required}
          onChange={(event) => onChange(event.target.value)}
        >
          <option value="">使用默认值</option>
          {field.choices.map((choice) => (
            <option key={choice} value={choice}>
              {optionChoices[field.key]?.[choice] ?? choice}
            </option>
          ))}
        </select>
      ) : (
        <input
          value={text}
          required={field.required}
          type={field.type === "integer" ? "number" : "text"}
          min={field.minimum}
          max={field.maximum}
          onChange={(event) => onChange(event.target.value)}
        />
      )}
    </label>
  );
}
function StringArrayField({field, value, onChange}: {
  field: OptionDescriptor; value: Value | undefined; onChange: (value: string) => void;
}) {
  // Keep a trailing newline while typing the next item; empty lines are not
  // persisted as archive paths or engine arguments.
  const [draft, setDraft] = useState<string | null>(null);
  return <label className="field">
    {field.label}
    <textarea rows={3} value={draft ?? (Array.isArray(value) ? value.join("\n") : "")} required={field.required}
      onChange={(event) => {setDraft(event.target.value); onChange(event.target.value);}}
      onBlur={() => setDraft(null)} />
  </label>;
}
const optionChoices: Record<string, Record<string, string>> = {
  textEncoding: {cp932: "日文（CP932）", gbk: "简体中文（GBK）"},
  language: {chs: "简体中文（汉化资源）", original: "原版资源"},
  engineMode: {KIRIKIRI2: "吉里吉里 2", KIRIKIRI_Z: "吉里吉里 Z"},
  steamService: {DISABLED: "禁用", LOCAL: "本地兼容服务"},
};
const optionLabels: Record<string, string> = {
  textEncoding: "脚本文字编码",
  engineMode: "引擎模式",
  overlayArchives: "资源补丁归档（每行一个路径，后者优先）",
  engineArguments: "引擎启动参数（每行一项）",
  steamService: "Steam 服务",
  engineId: "引擎",
  gameId: "游戏版本",
  root: "游戏资源目录",
  language: "语言",
  platform: "原游戏平台",
  extra: "版本说明",
  guiOptions: "游戏界面选项",
  filename: "识别文件",
  entryPath: "启动程序",
  memoryMB: "内存 (MB)",
  cycles: "运行速度",
  cpuType: "处理器",
  machine: "机型",
  sound: "声音",
};
