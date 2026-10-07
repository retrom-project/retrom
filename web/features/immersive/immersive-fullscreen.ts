export async function requestImmersiveFullscreen(documentObject: Document = document) {
  if (documentObject.fullscreenElement) {return true;}
  const root = documentObject.documentElement;
  if (typeof root.requestFullscreen !== "function") {return false;}
  try {
    await root.requestFullscreen();
    return true;
  } catch {
    return false;
  }
}
