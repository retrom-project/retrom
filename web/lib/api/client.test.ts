import { expect, it } from "vitest";
import { ApiError, result } from "./client";

it.each([
  ["VERSION_CONFLICT", "Item changed; refresh and try again", "数据已变化或与现有记录冲突，请刷新后重试。"],
  ["VERSION_CONFLICT", "存档已在另一个页面更新。当前草稿已保留，重新同步不会强行覆盖，请先导出备份。", "存档已在另一个页面更新。当前草稿已保留，重新同步不会强行覆盖，请先导出备份。"],
  ["TAG_NAME_CONFLICT", "A tag with this name already exists", "A tag with this name already exists"],
  ["FORBIDDEN", "Operation is not permitted", "Operation is not permitted"],
])("keeps %s machine fields while applying only its defined display mapping", (code, message, expected) => {
  let caught: unknown;
  try {
    result({ error: { code, message }, response: new Response(null, { status: 409 }) });
  } catch (error) {
    caught = error;
  }
  expect(caught).toBeInstanceOf(ApiError);
  expect(caught).toMatchObject({ code, status: 409, message: expected });
});
