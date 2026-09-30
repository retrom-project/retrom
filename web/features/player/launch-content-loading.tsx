import {ContentLoadingField} from "./content-loading-field";
import type {ContentLoading, ContentLoadingCapability} from "./content-loading";
import {useSyncExternalStore} from "react";
import {useAuth} from "@/features/auth/auth-provider";
import {readContentLoading, resolveContentLoading, subscribeContentLoading} from "./content-loading";

type LoadingCore = {contentLoading?: ContentLoadingCapability | null};

export function launchLoadingCount(selectedCore?: LoadingCore, savedCore?: LoadingCore) {
  const selected = selectedCore?.contentLoading, saved = savedCore?.contentLoading;
  return Number(Boolean(selected)) + Number(Boolean(saved && saved !== selected));
}

export function LaunchContentLoadingSummary({selectedCore, savedCore}: {selectedCore?: LoadingCore; savedCore?: LoadingCore}) {
  const {context} = useAuth();
  const mode = useSyncExternalStore<ContentLoading>(subscribeContentLoading, () => readContentLoading(context.user?.userId), () => "ON_DEMAND");
  const describe = (capability: LoadingCore["contentLoading"]) => resolveContentLoading(capability, mode, "PRODUCT") === "PRELOAD" ? "下载完成后开始" : "按需加载";
  const selected = selectedCore?.contentLoading, saved = savedCore?.contentLoading;
  const text = selected && saved && selected !== saved ? `重新开始：${describe(selected)}；继续：${describe(saved)}` : describe(selected ?? saved);
  return <span title={text}>{text}</span>;
}

export function LaunchContentLoading({selectedCore, savedCore}: {selectedCore?: LoadingCore; savedCore?: LoadingCore}) {
  const selected = selectedCore?.contentLoading;
  const saved = savedCore?.contentLoading;
  const separate = Boolean(savedCore && (selected ?? null) !== (saved ?? null));
  return <>
    <ContentLoadingField capability={selected} label={separate ? "重新开始的内容加载" : "内容加载"} />
    {separate ? <ContentLoadingField capability={saved} label="从存档继续的内容加载" /> : null}
  </>;
}
