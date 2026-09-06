"use client";

import type { Ref } from "react";
import { AppIcon } from "@/components/app-icon";
import { usePhoneLayout } from "@/features/mobile/phone-layout";

export function LibraryFilterTrigger({ count, expanded, onOpen, buttonRef }: {
  count: number;
  expanded: boolean;
  onOpen: () => void;
  buttonRef: Ref<HTMLButtonElement>;
}) {
  const phone = usePhoneLayout();
  const label = `筛选与排序${count ? ` · ${count}` : ""}`;
  return <button
    ref={buttonRef}
    className="button secondary library-mobile-filter-trigger"
    type="button"
    aria-label={label}
    data-filter-count={count || undefined}
    aria-expanded={expanded}
    onClick={onOpen}
  >
    <AppIcon name={phone ? "filter" : "settings"} />
    {phone ? null : label}
  </button>;
}
