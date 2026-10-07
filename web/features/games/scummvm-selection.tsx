"use client";
import { useToast } from "@/components/toast-provider";
import { useState } from "react";
import { api, result } from "@/lib/api/client";
import type { Schema } from "@/lib/api/types";
export function ScummvmSelection({
  gameId,
  onSelect,
}: {
  gameId: string;
  onSelect: (options: Schema<"ScummvmCandidate">["options"]) => void;
}) {
  const [candidates, setCandidates] =
    useState<Schema<"ScummvmCandidates"> | null>(null);
  const { notify } = useToast();
  const [busy, setBusy] = useState(false);
  const [selected, setSelected] = useState("");
  async function identify() {
    setBusy(true);
    try {
      const resultData = result(
        await api.POST("/api/v1/admin/games/{gameId}/runtime-options/scummvm", {
          params: { path: { gameId } },
        }),
      );
      setCandidates(resultData);
      if (resultData.automaticSelection) {
        choose(resultData.automaticSelection, resultData);
      }
      notify({
        tone: resultData.candidates.length ? "good" : "warn",
        message: resultData.automaticSelection
          ? "已识别游戏并填入运行配置，请保存更改。"
          : `识别完成，找到 ${resultData.candidates.length} 个候选。`,
      });
    } catch (failure) {
      notify({ tone: "bad", message: failure instanceof Error ? failure.message : "游戏识别失败，请重试。" });
    } finally {
      setBusy(false);
    }
  }
  function choose(id: string, data = candidates) {
    const candidate = data?.candidates.find((item) => item.id === id);
    if (!candidate || candidate.blocker) {
      return;
    }
    setSelected(id);
    onSelect(candidate.options);
  }
  return (
    <div className="stack">
      <button
        type="button"
        className="button secondary"
        disabled={busy}
        onClick={() => void identify()}
      >
        {busy ? "正在识别游戏…" : "识别 ScummVM 游戏"}
      </button>
      {candidates ? (
        <fieldset>
          <legend>识别结果</legend>
          {candidates.candidates.length ? (
            candidates.candidates.map((candidate) => (
              <label className="field" key={candidate.id}>
                <span>
                  <input
                    type="radio"
                    name="scummvm-candidate"
                    checked={selected === candidate.id}
                    disabled={!!candidate.blocker}
                    onChange={() => choose(candidate.id)}
                  />
                  {candidate.description}
                </span>
                {candidate.blocker ? (
                  <span className="restore-reason">
                    {blockers[candidate.blocker]}
                  </span>
                ) : null}
              </label>
            ))
          ) : (
            <p>未识别到可用游戏。请核对游戏资源是否完整。</p>
          )}
        </fieldset>
      ) : null}
    </div>
  );
}
const blockers = {
  UNKNOWN_VARIANT: "无法确认此游戏版本。",
  UNSUPPORTED_GAME: "当前核心不支持此游戏。",
  ENGINE_UNAVAILABLE: "所需引擎当前不可用。",
};
