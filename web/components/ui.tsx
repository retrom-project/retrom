import Link from "next/link";
import type { ReactNode } from "react";

export function PageHeader({ eyebrow, title, description, actions }: { eyebrow?: string; title: string; description: string; actions?: ReactNode }) {
  return (
    <header className="page-header">
      <div>{eyebrow ? <p className="eyebrow">{eyebrow}</p> : null}<h1>{title}</h1><p>{description}</p></div>
      {actions ? <div className="header-actions">{actions}</div> : null}
    </header>
  );
}

export function ButtonLink({ href, children, secondary = false }: { href: string; children: ReactNode; secondary?: boolean }) {
  return <Link className={secondary ? "button secondary" : "button"} href={href}>{children}</Link>;
}

export function StatusBadge({ tone = "neutral", children }: { tone?: "good" | "warn" | "bad" | "info" | "neutral"; children: ReactNode }) {
  return <span className={`status ${tone}`}><i />{children}</span>;
}

export function EmptyState({ title, description, action }: { title: string; description: string; action?: ReactNode }) {
  return <div className="empty"><span aria-hidden="true">◇</span><h2>{title}</h2><p>{description}</p>{action}</div>;
}

export function Kpi({ label, value, note, tone = "purple" }: { label: string; value: string | number; note: string; tone?: "purple" | "cyan" | "amber" | "slate" }) {
  return <article className={`kpi ${tone}`}><div><span>{label}</span><strong>{value}</strong></div><p>{note}</p></article>;
}

export function FilterBar({ placeholder = "搜索游戏、平台或任务…", children }: { placeholder?: string; children?: ReactNode }) {
  return <div className="filter-bar"><label className="search"><span aria-hidden="true">⌕</span><input aria-label="搜索" placeholder={placeholder} /></label>{children}</div>;
}
