export type ContainedSize = { width: number; height: number };

export function containSize(
  viewportWidth: number,
  viewportHeight: number,
  contentWidth: number,
  contentHeight: number
): ContainedSize | null {
  if (![viewportWidth, viewportHeight, contentWidth, contentHeight].every((value) => Number.isFinite(value) && value > 0)) return null;
  const contentRatio = contentWidth / contentHeight;
  if (contentRatio >= viewportWidth / viewportHeight) {
    return { width: viewportWidth, height: viewportWidth / contentRatio };
  }
  return { width: viewportHeight * contentRatio, height: viewportHeight };
}

export function fitCanvasToViewport(canvas: HTMLCanvasElement, viewportWidth: number, viewportHeight: number) {
  const size = containSize(viewportWidth, viewportHeight, canvas.width, canvas.height);
  if (!size) return false;
  canvas.style.setProperty("width", `${size.width}px`, "important");
  canvas.style.setProperty("height", `${size.height}px`, "important");
  canvas.style.setProperty("max-width", "none", "important");
  canvas.style.setProperty("max-height", "none", "important");
  return true;
}

export function installCanvasContain(frameDocument: Document) {
  const frameWindow = frameDocument.defaultView;
  if (!frameWindow) return () => undefined;
  const fit = () => {
    const canvas = frameDocument.querySelector<HTMLCanvasElement>("canvas");
    if (!canvas) return;
    fitCanvasToViewport(canvas, frameWindow.innerWidth, frameWindow.innerHeight);
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
  return () => {
    mutationObserver.disconnect();
    resizeObserver?.disconnect();
    frameWindow.removeEventListener("resize", fit);
  };
}
