export function scanErrorMessage(error: string) {
  return error === "CONTENT_NORMALIZATION_FAILED"
    ? "内容转换失败，请检查游戏来源与补丁文件；来源文件保持不变。"
    : error;
}
