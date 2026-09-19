// A locator screenshot includes overlaid Host controls. Read only the native
// canvas bitmap when a case makes assertions about exact game coordinates.
export async function canvasImage(canvas) {
  const data = await canvas.evaluate(source => source.toDataURL("image/png"));
  if (!data.startsWith("data:image/png;base64,")) throw new Error("ACCEPTANCE_CANVAS_PNG_REQUIRED");
  return Buffer.from(data.slice("data:image/png;base64,".length), "base64");
}
