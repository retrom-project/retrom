import { ButtonLink, PageHeader } from "@/components/ui";
import { UploadPicker } from "@/features/imports/upload-picker";
import { backendJSON, type ListResponse } from "@/lib/backend";

export const metadata = { title: "导入游戏" };

type Instance = { id: string; name: string; platformName: string; defaultCoreName: string };

export default async function NewImportPage() {
  const result = await backendJSON<ListResponse<Instance>>("/api/v1/admin/platform-instances");
  const directories = result.items.map((item) => ({ id: item.id, name: item.name, platformName: item.platformName, coreName: item.defaultCoreName }));
  return (
    <div className="import-workflow-page import-new-page">
      <PageHeader eyebrow="New import" title="导入游戏" description="通过三个明确阶段提交内容。导入任务创建后进入“任务进度”，不会直接跳到尚未准备好的审核队列。" actions={<ButtonLink href="/admin/imports" secondary>返回总览</ButtonLink>} />
      <UploadPicker directories={directories} />
    </div>
  );
}
