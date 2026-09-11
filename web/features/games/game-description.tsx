const descriptionLimit = 160;

export function summarizeGameDescription(description: string) {
  const characters = Array.from(description.replace(/\s+/gu, " ").trim());
  return characters.length > descriptionLimit
    ? `${characters.slice(0, descriptionLimit - 3).join("")}...`
    : characters.join("");
}

export function GameDescription({ description, className }: { description: string; className: string }) {
  const summary = summarizeGameDescription(description);
  return summary ? <div className={className}><p>{summary}</p></div> : null;
}
