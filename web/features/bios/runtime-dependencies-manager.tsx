"use client";

import { useRef, useState, type KeyboardEvent } from "react";
import { BIOSManager, type BIOSListResponse } from "./bios-manager";
import {RuntimeAssetPackManager, type RuntimeAssetPackList, type RuntimeTargetList} from "./runtime-asset-pack-manager";
import type { BIOSFilters, BIOSQuickFilter } from "./runtime-dependencies";

type Scope = "REQUIRED_BY_LIBRARY" | "FULL_CATALOG";
type Tab = "bios" | "rpgmaker";

function setTabURL(tab: Tab) {
  const params = new URLSearchParams(window.location.search);
  params.set("tab", tab);
  window.history.replaceState(window.history.state, "", `${window.location.pathname}${params.size ? `?${params}` : ""}`);
}

export function RuntimeDependenciesManager({
  initialBIOS,
  initialPackList,
  initialRuntimeTargets,
  initialTab,
  initialScope,
  initialFilters,
}: {
  initialBIOS: BIOSListResponse;
  initialPackList: RuntimeAssetPackList;
  initialRuntimeTargets: RuntimeTargetList;
  initialTab: Tab;
  initialScope: Scope;
  initialFilters: Partial<BIOSFilters> & { quick: BIOSQuickFilter };
}) {
  const [tab, setTab] = useState<Tab>(initialTab);
  const tabs = useRef<Array<HTMLButtonElement | null>>([]);
  const chooseTab = (value: Tab) => {
    setTab(value);
    setTabURL(value);
  };
  const onTabKeyDown = (event: KeyboardEvent<HTMLButtonElement>, index: number) => {
    let target = index;
    if (event.key === "ArrowRight") {target = (index + 1) % 2;}
    else if (event.key === "ArrowLeft") {target = (index + 1) % 2;}
    else if (event.key === "Home") {target = 0;}
    else if (event.key === "End") {target = 1;}
    else {return;}
    event.preventDefault();
    const value: Tab = target === 0 ? "bios" : "rpgmaker";
    chooseTab(value);
    tabs.current[target]?.focus();
  };
  return <>
    <div className="runtime-dependency-tabs" role="tablist" aria-label="运行依赖类型">
      <button ref={(element) => {tabs.current[0] = element;}} type="button" role="tab" aria-selected={tab === "bios"} tabIndex={tab === "bios" ? 0 : -1} className={tab === "bios" ? "is-active" : ""} onClick={() => chooseTab("bios")} onKeyDown={(event) => onTabKeyDown(event, 0)}>BIOS 文件</button>
      <button ref={(element) => {tabs.current[1] = element;}} type="button" role="tab" aria-selected={tab === "rpgmaker"} tabIndex={tab === "rpgmaker" ? 0 : -1} className={tab === "rpgmaker" ? "is-active" : ""} onClick={() => chooseTab("rpgmaker")} onKeyDown={(event) => onTabKeyDown(event, 1)}>RPG Maker 运行包</button>
    </div>
    <div role="tabpanel" aria-label={tab === "bios" ? "BIOS 文件" : "RPG Maker 运行包"}>
      {tab === "bios"
        ? <BIOSManager initialResponse={initialBIOS} initialScope={initialScope} initialFilters={initialFilters} />
        : <RuntimeAssetPackManager initialList={initialPackList} initialRuntimeTargets={initialRuntimeTargets} />}
    </div>
  </>;
}
