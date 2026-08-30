export const immersiveMenuOrder = ["取消", "创建存档", "退出游戏"] as const;

export type ImmersiveMenuLabel = typeof immersiveMenuOrder[number];
export type ImmersiveMenuDirection = "left" | "right";

export async function selectImmersiveMenuItem(
  target: ImmersiveMenuLabel,
  currentSelection: () => Promise<string | null>,
  pressDirection: (direction: ImmersiveMenuDirection) => Promise<void>,
) {
  for (let attempt = 0; attempt < immersiveMenuOrder.length * 2; attempt += 1) {
    const current = await currentSelection();
    if (current === target) {return;}
    const currentIndex = immersiveMenuOrder.indexOf(current as ImmersiveMenuLabel);
    const targetIndex = immersiveMenuOrder.indexOf(target);
    if (currentIndex < 0) {throw new Error(`IMMERSIVE_MENU_SELECTION_INVALID:${current ?? "none"}`);}
    const rightDistance = (targetIndex - currentIndex + immersiveMenuOrder.length) % immersiveMenuOrder.length;
    const leftDistance = (currentIndex - targetIndex + immersiveMenuOrder.length) % immersiveMenuOrder.length;
    await pressDirection(rightDistance <= leftDistance ? "right" : "left");
  }
  throw new Error(`IMMERSIVE_MENU_ITEM_NOT_SELECTABLE:${target}`);
}
