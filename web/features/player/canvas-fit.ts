export type ContainedSize = { width: number; height: number };

export function containSize(
  viewportWidth: number,
  viewportHeight: number,
  contentWidth: number,
  contentHeight: number
): ContainedSize | null {
  if (![viewportWidth, viewportHeight, contentWidth, contentHeight].every((value) => Number.isFinite(value) && value > 0)) {return null;}
  const contentRatio = contentWidth / contentHeight;
  if (contentRatio >= viewportWidth / viewportHeight) {
    return { width: viewportWidth, height: viewportWidth / contentRatio };
  }
  return { width: viewportHeight * contentRatio, height: viewportHeight };
}

export function fitCanvasToViewport(canvas: HTMLCanvasElement, viewportWidth: number, viewportHeight: number, aspectRatio?: number) {
  const hasRuntimeAspect = typeof aspectRatio === "number" && Number.isFinite(aspectRatio) && aspectRatio > 0;
  const size = hasRuntimeAspect
    ? containSize(viewportWidth, viewportHeight, aspectRatio, 1)
    : containSize(viewportWidth, viewportHeight, canvas.width, canvas.height);
  if (!size) {return false;}
  canvas.style.setProperty("width", `${size.width}px`, "important");
  canvas.style.setProperty("height", `${size.height}px`, "important");
  canvas.style.setProperty("max-width", "none", "important");
  canvas.style.setProperty("max-height", "none", "important");
  canvas.style.setProperty("justify-self", "center", "important");
  canvas.style.setProperty("align-self", "center", "important");
  canvas.style.setProperty("position", "absolute", "important");
  canvas.style.setProperty("left", `${(viewportWidth - size.width) / 2}px`, "important");
  canvas.style.setProperty("top", `${(viewportHeight - size.height) / 2}px`, "important");
  return true;
}

export function installCanvasContain(frameDocument: Document, getAspectRatio: () => number | undefined = () => undefined) {
  const frameWindow = frameDocument.defaultView;
  if (!frameWindow) {return { refresh: () => undefined, cleanup: () => undefined };}
  const fit = () => {
    const canvas = frameDocument.querySelector<HTMLCanvasElement>("canvas");
    if (!canvas) {return;}
    fitCanvasToViewport(canvas, frameWindow.innerWidth, frameWindow.innerHeight, getAspectRatio());
  };
  const mutationObserver = new frameWindow.MutationObserver(fit);
  mutationObserver.observe(frameDocument.documentElement, {
    attributes: true,
    attributeFilter: ["width", "height"],
    childList: true,
    subtree: true
  });
  const ResizeObserverConstructor = frameWindow.ResizeObserver;
  const resizeObserver = ResizeObserverConstructor ? new ResizeObserverConstructor(fit) : null;
  resizeObserver?.observe(frameDocument.documentElement);
  frameWindow.addEventListener("resize", fit);
  fit();
  return { refresh: fit, cleanup: () => {
    mutationObserver.disconnect();
    resizeObserver?.disconnect();
    frameWindow.removeEventListener("resize", fit);
  } };
}
