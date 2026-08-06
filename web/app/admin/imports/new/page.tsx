import { ButtonLink, PageHeader } from "@/components/ui";
import { UploadPicker } from "@/features/imports/upload-picker";
import { backendJSON, type ListResponse } from "@/lib/backend";

export const metadata = { title: "新建导入" };

type Instance = { id: string; name: string; platformName: string; defaultCoreName: string };

export default async function NewImportPage() {
  const result = await backendJSON<ListResponse<Instance>>("/api/v1/admin/platform-instances");
  const directories = result.items.map((item) => ({ id: item.id, name: item.name, platformName: item.platformName, coreName: item.defaultCoreName }));
  return (
    <>
      <PageHeader eyebrow="New import" title="导入文件 / 目录" description="内容只来自浏览器选择，不会扫描或暴露宿主机任意路径。" actions={<ButtonLink href="/admin/imports" secondary>返回总览</ButtonLink>} />
      <div className="stepper"><div className="step is-active"><i>1</i>选择内容</div><div className="step"><i>2</i>确认配置</div><div className="step"><i>3</i>上传并验证</div></div>
      <UploadPicker directories={directories} />
    </>
  );
}
