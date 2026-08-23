import type { ControllerDirection } from "./types";

const focusableSelector = [
  "a[href]",
  "button:not([disabled])",
  "select:not([disabled])",
  "input:not([disabled]):not([type='hidden'])",
  "[tabindex]:not([tabindex='-1'])",
].join(",");

const textInputTypes = new Set(["email", "password", "search", "tel", "text", "url"]);

function inputRequiresKeyboard(element: HTMLElement) {
  if (element instanceof HTMLTextAreaElement || element.isContentEditable) {return true;}
  return element instanceof HTMLInputElement && textInputTypes.has(element.type);
}

function insideHiddenTree(element: HTMLElement) {
  return Boolean(element.closest("[inert],[aria-hidden='true'],[hidden]"));
}

function rendered(element: HTMLElement) {
  const rect = element.getBoundingClientRect();
  const style = window.getComputedStyle(element);
  return rect.width > 0 && rect.height > 0 && style.visibility !== "hidden" && style.display !== "none";
}

export function isControllerFocusable(element: HTMLElement) {
  return !inputRequiresKeyboard(element) && !insideHiddenTree(element) &&
    element.dataset.gamepadDisabled !== "true" && rendered(element);
}

export function activeControllerScope(root: ParentNode = document) {
  const declared = Array.from(root.querySelectorAll<HTMLElement>(
    "[data-gamepad-scope][data-gamepad-open='true'],[role='dialog'][aria-modal='true']",
  )).filter(rendered);
  return declared.at(-1) ?? root;
}

export function controllerCandidates(scope: ParentNode = activeControllerScope()) {
  return Array.from(scope.querySelectorAll<HTMLElement>(focusableSelector)).filter(isControllerFocusable);
}

function explicitNeighbor(current: HTMLElement, direction: ControllerDirection, candidates: HTMLElement[]) {
  const key = current.dataset[`gamepad${direction[0]!.toUpperCase()}${direction.slice(1)}` as keyof DOMStringMap];
  if (!key) {return null;}
  return candidates.find((candidate) => candidate.dataset.gamepadKey === key) ?? null;
}

type CandidateScore = readonly [number, number, number, number, number, number];

function compareScore(left: CandidateScore, right: CandidateScore) {
  for (let index = 0; index < left.length; index += 1) {
    if (left[index] !== right[index]) {return left[index]! - right[index]!;}
  }
  return 0;
}

type RectProjection = Readonly<{ mainStart: number; mainEnd: number; crossStart: number; crossEnd: number }>;

function projectRect(rect: DOMRect, horizontal: boolean): RectProjection {
  if (horizontal) {
    return { mainStart: rect.left, mainEnd: rect.right, crossStart: rect.top, crossEnd: rect.bottom };
  }
  return { mainStart: rect.top, mainEnd: rect.bottom, crossStart: rect.left, crossEnd: rect.right };
}

function forwardGeometry(current: RectProjection, candidate: RectProjection, forward: boolean) {
  const currentCenter = (current.mainStart + current.mainEnd) / 2;
  const candidateCenter = (candidate.mainStart + candidate.mainEnd) / 2;
  if (forward && candidateCenter < currentCenter + 4) {return null;}
  if (!forward && candidateCenter > currentCenter - 4) {return null;}
  const mainDistance = forward
    ? candidate.mainStart - current.mainEnd
    : current.mainStart - candidate.mainEnd;
  return { currentCenter, candidateCenter, mainDistance: Math.max(0, mainDistance) };
}

function directionGeometry(currentRect: DOMRect, candidateRect: DOMRect, direction: ControllerDirection) {
  const horizontal = direction === "left" || direction === "right";
  const forward = direction === "right" || direction === "down";
  const current = projectRect(currentRect, horizontal);
  const candidate = projectRect(candidateRect, horizontal);
  const main = forwardGeometry(current, candidate, forward);
  if (!main) {return null;}
  const currentCrossStart = current.crossStart;
  const currentCrossEnd = current.crossEnd;
  const candidateCrossStart = candidate.crossStart;
  const candidateCrossEnd = candidate.crossEnd;
  const intersects = candidateCrossStart <= currentCrossEnd && candidateCrossEnd >= currentCrossStart;
  const crossGap = intersects
    ? 0
    : Math.min(Math.abs(candidateCrossStart - currentCrossEnd), Math.abs(currentCrossStart - candidateCrossEnd));
  const currentCrossCenter = (currentCrossStart + currentCrossEnd) / 2;
  const candidateCrossCenter = (candidateCrossStart + candidateCrossEnd) / 2;
  return { intersects, mainDistance: main.mainDistance, crossGap, centerDistance: Math.hypot(
    main.candidateCenter - main.currentCenter,
    candidateCrossCenter - currentCrossCenter,
  ) };
}

function candidateScore(
  current: HTMLElement,
  candidate: HTMLElement,
  direction: ControllerDirection,
  order: number,
): CandidateScore | null {
  const geometry = directionGeometry(current.getBoundingClientRect(), candidate.getBoundingClientRect(), direction);
  if (!geometry) {return null;}
  const priority = Number(candidate.dataset.gamepadPriority ?? "0");
  return [
    geometry.intersects ? 0 : 1,
    geometry.mainDistance,
    geometry.crossGap,
    geometry.centerDistance,
    Number.isFinite(priority) ? -priority : 0,
    order,
  ];
}

export function findControllerNeighbor(
  current: HTMLElement,
  direction: ControllerDirection,
  candidates = controllerCandidates(),
) {
  const explicit = explicitNeighbor(current, direction, candidates);
  if (explicit) {return explicit;}
  let best: { element: HTMLElement; score: CandidateScore } | null = null;
  for (const [order, candidate] of candidates.entries()) {
    if (candidate === current) {continue;}
    const score = candidateScore(current, candidate, direction, order);
    if (score && (!best || compareScore(score, best.score) < 0)) {best = { element: candidate, score };}
  }
  return best === null ? null : best.element;
}

export function focusControllerElement(element: HTMLElement) {
  element.focus({ preventScroll: true });
  const rect = element.getBoundingClientRect();
  if (rect.top < 0 || rect.left < 0 || rect.bottom > window.innerHeight || rect.right > window.innerWidth) {
    element.scrollIntoView({ block: "nearest", inline: "nearest", behavior: "instant" });
  }
}

export function focusControllerDefault(scope: ParentNode = activeControllerScope()) {
  const candidates = controllerCandidates(scope);
  const preferred = candidates.find((candidate) => candidate.dataset.gamepadDefault === "true") ?? candidates[0] ?? null;
  if (preferred) {focusControllerElement(preferred);}
  return preferred;
}

export function moveControllerFocus(direction: ControllerDirection) {
  const scope = activeControllerScope();
  const candidates = controllerCandidates(scope);
  const active = document.activeElement instanceof HTMLElement && candidates.includes(document.activeElement)
    ? document.activeElement
    : focusControllerDefault(scope);
  if (!active) {return null;}
  const next = findControllerNeighbor(active, direction, candidates);
  if (next) {focusControllerElement(next);}
  return next ?? active;
}

type EditableControlState = Readonly<
  | { kind: "select"; element: HTMLSelectElement; originalIndex: number }
  | { kind: "range"; element: HTMLInputElement; originalValue: string }
>;

let editableControl: EditableControlState | null = null;

export function activateControllerFocus() {
  const active = document.activeElement;
  if (!(active instanceof HTMLElement) || !isControllerFocusable(active)) {return false;}
  if (active instanceof HTMLSelectElement) {
    if (!editableControl) {editableControl = { kind: "select", element: active, originalIndex: active.selectedIndex };}
    else {editableControl = null; active.dispatchEvent(new Event("change", { bubbles: true }));}
    return true;
  }
  if (active instanceof HTMLInputElement && active.type === "range") {
    if (!editableControl) {editableControl = { kind: "range", element: active, originalValue: active.value };}
    else {editableControl = null; active.dispatchEvent(new Event("change", { bubbles: true }));}
    return true;
  }
  active.click();
  return true;
}

function changeRangeController(range: HTMLInputElement, delta: number, groupJump: boolean) {
  const minimum = Number(range.min || "0");
  const maximum = Number(range.max || "100");
  const normalStep = Number(range.dataset.gamepadStep ?? range.step ?? "1");
  const groupStep = Number(range.dataset.gamepadGroupStep ?? String(normalStep * 4));
  const step = groupJump ? groupStep : normalStep;
  range.value = String(Math.max(minimum, Math.min(maximum, Number(range.value) + delta * step)));
  range.dispatchEvent(new Event("input", { bubbles: true }));
}

function changeSelectController(select: HTMLSelectElement, delta: number, groupJump: boolean) {
  let next = select.selectedIndex + delta * (groupJump ? 10 : 1);
  next = Math.max(0, Math.min(select.options.length - 1, next));
  while (select.options[next]?.disabled && next !== select.selectedIndex) {
    const candidate = next + delta;
    if (candidate < 0 || candidate >= select.options.length) {break;}
    next = candidate;
  }
  select.selectedIndex = next;
  select.dispatchEvent(new Event("input", { bubbles: true }));
}

export function changeEditableController(direction: ControllerDirection, groupJump = false) {
  if (!editableControl) {return false;}
  const delta = direction === "up" || direction === "left" ? -1 : 1;
  if (editableControl.kind === "range") {
    changeRangeController(editableControl.element, delta, groupJump);
  } else {
    changeSelectController(editableControl.element, delta, groupJump);
  }
  return true;
}

export function controllerBackInScope() {
  if (editableControl) {
    if (editableControl.kind === "select") {
      editableControl.element.selectedIndex = editableControl.originalIndex;
    } else {
      editableControl.element.value = editableControl.originalValue;
      editableControl.element.dispatchEvent(new Event("input", { bubbles: true }));
    }
    editableControl = null;
    return true;
  }
  const scope = activeControllerScope();
  if (scope === document) {return false;}
  const back = scope.querySelector<HTMLElement>(
    "[data-gamepad-back='true'],button[aria-label*='关闭'],button[aria-label*='取消']",
  );
  if (!back || !isControllerFocusable(back)) {return false;}
  back.click();
  return true;
}

export function controllerGroupAction(next: boolean) {
  const active = document.activeElement;
  if ((active instanceof HTMLSelectElement || active instanceof HTMLInputElement) && editableControl) {
    return changeEditableController(next ? "down" : "up", true);
  }
  if (!(active instanceof HTMLElement)) {return false;}
  const selector = next ? active.dataset.gamepadNextGroup : active.dataset.gamepadPreviousGroup;
  const target = selector ? document.querySelector<HTMLElement>(`[data-gamepad-key='${CSS.escape(selector)}']`) : null;
  if (target && isControllerFocusable(target)) {focusControllerElement(target); return true;}
  return false;
}
