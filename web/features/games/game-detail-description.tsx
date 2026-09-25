"use client";

import { useId, useState } from "react";

const PREVIEW_LENGTH = 320;

export function GameDetailDescription({ description }: { description: string }) {
  const [expanded, setExpanded] = useState(false);
  const id = useId();
  const characters = Array.from(description.trim());
  const long = characters.length > PREVIEW_LENGTH;
  const text = long && !expanded ? `${characters.slice(0, PREVIEW_LENGTH).join("")}…` : description;
  return <div className="game-detail-description">
    <p id={id}>{characters.length ? text : "尚未填写游戏简介。"}</p>
    {long ? <button className="button secondary" type="button" aria-expanded={expanded} aria-controls={id} onClick={() => setExpanded(!expanded)}>{expanded ? "收起简介" : "展开完整简介"}</button> : null}
  </div>;
}
