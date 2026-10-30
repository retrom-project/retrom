/** Encode new save images once, before retaining the draft for upload or retry. */
export async function prepareSaveScreenshot(image: Blob | null): Promise<Blob | null> {
  if (!image) { return null; }
  let bitmap: ImageBitmap | undefined;
  try {
    bitmap = await createImageBitmap(image);
    const canvas = document.createElement("canvas");
    canvas.height = Math.min(bitmap.height, 540);
    canvas.width = Math.max(1, Math.round(bitmap.width * canvas.height / bitmap.height));
    const context = canvas.getContext("2d", { alpha: false });
    if (!context) { return null; }
    context.fillStyle = "#000";
    context.fillRect(0, 0, canvas.width, canvas.height);
    context.imageSmoothingEnabled = true;
    context.imageSmoothingQuality = "high";
    context.drawImage(bitmap, 0, 0, canvas.width, canvas.height);
    return await new Promise<Blob | null>((resolve) => {
      canvas.toBlob((blob) => resolve(blob?.size && blob.type === "image/jpeg" ? blob : null), "image/jpeg", 0.95);
    });
  } catch {
    // Screenshot failure must not discard the player's checkpoint.
    return null;
  } finally {
    bitmap?.close();
  }
}
