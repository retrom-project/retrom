export const contentRequirementReasons: Record<string, {title: string; explanation: string; action: string}> = {
  THREEDS_ENCRYPTED_CONTENT: {title: "3DS 镜像尚未解密", explanation: "当前核心只能读取已解密的 3DS 镜像。", action: "请提供已解密的兼容镜像；重试相同内容无法解决。"},
  THREEDS_CONTAINER_INVALID: {title: "3DS 镜像结构无效或不完整", explanation: "无法读取有效的游戏分区，或分区范围超过文件边界。", action: "请检查镜像完整性并更换有效内容。"},
  FLYCAST_GDROM_UNSUPPORTED: {title: "当前目标不支持 GD-ROM 内容", explanation: "这个 ZIP 只提供安全芯片等附属文件，游戏还需要外部介质。", action: "请使用当前目标支持的完整卡带 ZIP；安装 BIOS 无法补齐游戏介质。"},
  FLYCAST_PLATFORM_MISMATCH: {title: "游戏硬件与所选目标不匹配", explanation: "核心的机器表将这个游戏归属于另一种硬件。", action: "请选择对应的硬件目标；当前未支持的硬件不能通过更换 BIOS 放行。"},
  FLYCAST_MACHINE_UNKNOWN: {title: "无法识别该街机名称", explanation: "ZIP 文件名没有对应当前核心的机器条目。", action: "请核对原始机器名及核心版本，不要使用游戏展示标题重命名 ROM。"},
  FLYCAST_ARCHIVE_INCOMPLETE: {title: "卡带 ZIP 缺少必需成员", explanation: "当前目标要求所有卡带成员位于同一个 ZIP，不查找外部 Parent。", action: "请按缺失成员补齐完整卡带内容后重新导入。"},
  FLYCAST_ROM_MISMATCH: {title: "卡带成员与当前核心要求不匹配", explanation: "归档内成员的大小或校验值与当前核心的机器表不同。", action: "请更换与当前核心版本匹配的 ROM 内容。"},
  FLYCAST_ARCHIVE_INVALID: {title: "卡带 ZIP 无效或损坏", explanation: "归档未通过完整性或安全检查。", action: "请检查原始归档，并重新提供完整的 ZIP。"},
  CONTENT_REQUIREMENTS_UNAVAILABLE: {title: "缺少当前内容检查依据", explanation: "现有内容缺少当前目标要求的检查事实。", action: "请重新导入内容以完成检查。"},
};
export const contentRequirementLabels = Object.fromEntries(
  Object.entries(contentRequirementReasons).map(([code, reason]) => [code, reason.title]),
);
