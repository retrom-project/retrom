export type ProductCheckpointPresentation = {
  text: "可创建存档" | "当前场景暂不可存档";
  tone: "synced" | "warning";
};

export function productCheckpointPresentation(available: boolean): ProductCheckpointPresentation {
  return available
    ? {text: "可创建存档", tone: "synced"}
    : {text: "当前场景暂不可存档", tone: "warning"};
}
