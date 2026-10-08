import { ApiError } from "@/lib/api/client";

export function tagSaveError(failure: unknown) {
  if (failure instanceof ApiError && failure.code === "TAG_NAME_CONFLICT") {
    return "已存在同名标签，请使用其他名称。";
  }
  if (failure instanceof ApiError && failure.code === "VERSION_CONFLICT") {
    return "标签已在其他页面修改，请刷新列表后重试。";
  }
  return failure instanceof Error ? failure.message : "保存标签失败，请重试。";
}
