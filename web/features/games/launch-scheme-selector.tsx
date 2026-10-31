"use client";
import {useCallback, useEffect} from "react";
import {api, result} from "@/lib/api/client";
import type {Schema} from "@/lib/api/types";
import {useResource} from "@/lib/use-resource";

export function LaunchSchemeSelector({gameId, value, onChange, onAvailable}: {
  gameId: string; value: Schema<"RuntimeConfig">; onChange: (value: Schema<"RuntimeConfig">) => void;
  onAvailable: (available: boolean) => void;
}) {
  const draft = JSON.stringify(value);
  const load = useCallback(async (signal?: AbortSignal) => result(await api.POST(
    "/api/v1/admin/games/{gameId}/runtime-options/launches", {
      signal, params: {path: {gameId}}, body: JSON.parse(draft) as Schema<"RuntimeConfig">,
    },
  )), [gameId, draft]);
  const options = useResource(load);
  const available = !!options.data?.items.length;
  useEffect(() => {if (!options.loading) {onAvailable(available);}}, [onAvailable, available, options.loading]);
  return <div>
    {available ? <label className="field">启动方案
      <select value={options.data?.selectedId ?? ""} disabled={options.loading || !!options.error}
        onChange={event => {
          const selected = options.data?.items.find(item => item.id === event.target.value);
          if (selected) {onChange(selected.configuration);}
        }}>
        {options.data?.items.map(item => <option key={item.id} value={item.id}>{item.label}</option>)}
      </select>
    </label> : null}
    {available ? <p className="workspace-note">保存后，新的运行使用所选方案。</p> : null}
    {options.loading ? <p role="status">正在读取启动方案…</p> : null}
    {options.error ? <p role="alert">无法读取启动方案。<button type="button" className="button secondary"
      onClick={options.reload}>重试读取启动方案</button></p> : null}
  </div>;
}
