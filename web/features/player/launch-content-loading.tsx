import {ContentLoadingField} from "./content-loading-field";
import type {ContentLoadingCapability} from "./content-loading";

type LoadingCore = {contentLoading?: ContentLoadingCapability | null};

export function launchLoadingCount(selectedCore?: LoadingCore, savedCore?: LoadingCore) {
  const selected = selectedCore?.contentLoading, saved = savedCore?.contentLoading;
  return Number(Boolean(selected)) + Number(Boolean(saved && saved !== selected));
}

export function LaunchContentLoading({selectedCore, savedCore, hideLabel = false}: {selectedCore?: LoadingCore; savedCore?: LoadingCore; hideLabel?: boolean}) {
  const selected = selectedCore?.contentLoading;
  const saved = savedCore?.contentLoading;
  const separate = Boolean(savedCore && (selected ?? null) !== (saved ?? null));
  return <>
    <ContentLoadingField capability={selected} label={separate ? "重新开始的内容加载" : "内容加载"} hideLabel={hideLabel && !separate} />
    {separate ? <ContentLoadingField capability={saved} label="从存档继续的内容加载" /> : null}
  </>;
}
