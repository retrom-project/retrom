import { ButtonLink, EmptyState } from "@/components/ui";

export default function NotFound() {
  return <EmptyState title="没有找到这个页面" description="链接可能已经失效，或资源尚未创建。" action={<ButtonLink href="/">返回首页</ButtonLink>} />;
}
