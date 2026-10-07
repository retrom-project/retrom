import type { Schema } from "@/lib/api/types";
import { ResponsiveSheet } from "@/components/responsive-sheet";

export function PlayerSaveChoice({
  open,
  save,
  onClose,
  onSelect,
}: {
  open: boolean;
  save: Schema<"Save"> | null;
  onClose: () => void;
  onSelect: (save: Schema<"Save"> | null) => void;
}) {
  return (
    <ResponsiveSheet
      open={open}
      title="保存存档"
      description="建立新的保存画面，或更新当前存档。"
      onClose={onClose}
    >
      <div className="stack">
        <button className="button" onClick={() => onSelect(null)}>
          新建存档
        </button>
        {save ? (
          <>
            <p className="muted">覆盖会更新「{save.name}」的进度和画面。</p>
            <button className="button secondary" onClick={() => onSelect(save)}>
              覆盖当前存档
            </button>
          </>
        ) : null}
      </div>
    </ResponsiveSheet>
  );
}
