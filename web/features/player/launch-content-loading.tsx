import {ContentLoadingField} from "./content-loading-field";
import type {ContentLoadingCapability} from "./content-loading";

type LoadingCore = {contentLoading?: ContentLoadingCapability | null};

export function LaunchContentLoading({selectedCore, savedCore}: {selectedCore?: LoadingCore; savedCore?: LoadingCore}) {
  const selected = selectedCore?.contentLoading;
  const saved = savedCore?.contentLoading;
  const separate = Boolean(savedCore && (selected ?? null) !== (saved ?? null));
  return <>
    <ContentLoadingField capability={selected} label={separate ? "重新开始的内容加载" : "内容加载"} />
    {separate ? <ContentLoadingField capability={saved} label="从存档继续的内容加载" /> : null}
  </>;
}
