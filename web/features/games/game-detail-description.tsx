export function GameDetailDescription({ description }: { description: string }) {
  const text = description.replace(/\\r\\n|\\n|\\r|\r\n?/g, "\n");
  return <div className="game-detail-description" role="region" aria-label="游戏简介" tabIndex={text.trim() ? 0 : undefined}>
    <p>{text.trim() ? text : "尚未填写游戏简介。"}</p>
  </div>;
}
