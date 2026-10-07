export function screenshotFileName(name: string, image: Blob) {
  return `${name}.${image.type === "image/jpeg" ? "jpg" : "png"}`;
}
