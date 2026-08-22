import type { ComponentProps } from "react";
import { ButtonLink, EmptyState } from "@/components/ui";
import { UploadPicker } from "./upload-picker";

type ImportSetupProps = ComponentProps<typeof UploadPicker>;

export function ImportSetup(props: ImportSetupProps) {
  if (props.directories.length === 0) {
    return <EmptyState
      title="还没有游戏目录"
      description="请先进入游戏目录，使用“一键创建推荐目录”建立可用目录，再回来导入游戏。"
      action={<ButtonLink href="/admin/platform-instances">前往游戏目录</ButtonLink>}
    />;
  }
  return <UploadPicker {...props} />;
}
