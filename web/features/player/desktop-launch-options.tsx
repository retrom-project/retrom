import {Fragment, useId, useRef, useState, type ReactNode} from "react";
import {LaunchContentLoading, launchLoadingCount} from "./launch-content-loading";
import {DOSProgramPicker} from "./dos-program-picker";
import type {CoreOption, DOSEntry} from "./launch-controls";

export function DesktopLaunchSettings({coreOptions, selectedCore, savedCoreId, isDOS, dosEntries, defaultDosEntry, dosEntry, onDOSChange}: {
  coreOptions: CoreOption[];
  selectedCore?: CoreOption;
  savedCoreId?: string;
  isDOS: boolean;
  dosEntries: DOSEntry[];
  defaultDosEntry: string | null;
  dosEntry: string | null;
  onDOSChange: (value: string | null) => void;
}) {
  const savedCore = savedCoreId === undefined ? undefined : coreOptions.find(core => core.coreId === savedCoreId) ?? {};
  const count = launchLoadingCount(selectedCore, savedCore);
  const tabbed = isDOS && count > 0;
  return <DesktopLaunchOptions loadingCount={count}
    loading={<LaunchContentLoading selectedCore={selectedCore} savedCore={savedCore} hideLabel={tabbed} />}
    program={isDOS ? <DOSProgramPicker defaultDosEntry={defaultDosEntry} dosEntries={dosEntries} onChange={onDOSChange} value={dosEntry} hideLabel={tabbed} /> : undefined} />;
}

export function DesktopLaunchOptions({loading, program, loadingCount}: {
  loading: ReactNode;
  program?: ReactNode;
  loadingCount: number;
}) {
  const tabbed = loadingCount > 0 && Boolean(program);
  const [active, setActive] = useState(0);
  const id = useId();
  const tabsRef = useRef<HTMLDivElement>(null);
  const options = [
    {label: "内容加载", content: loading},
    {label: "启动程序", content: program},
  ];
  return <div className={`launch-options${tabbed ? " is-tabbed" : ""}`}>
    {tabbed ? <div ref={tabsRef} className="launch-option-tabs" role="tablist" aria-label="启动设置">
      {options.map((option, index) => <Fragment key={option.label}>
        {index > 0 ? <span className="launch-option-divider" aria-hidden="true">/</span> : null}
        <button type="button" className="button secondary option-tab" role="tab" id={`${id}-tab-${index}`} aria-controls={`${id}-panel-${index}`} aria-selected={index === active} tabIndex={index === active ? 0 : -1} onClick={() => setActive(index)} onKeyDown={event => {
        const next = event.key === "Home" ? 0 : event.key === "End" ? 1
          : event.key === "ArrowLeft" || event.key === "ArrowRight" ? 1 - index : -1;
        if (next < 0) {return;}
        event.preventDefault(); setActive(next);
        tabsRef.current?.querySelectorAll<HTMLButtonElement>("button")[next]?.focus();
      }}>{option.label}</button></Fragment>)}
    </div> : null}
    {options.map((option, index) => <div key={option.label} id={`${id}-panel-${index}`} className={`launch-option-panel${tabbed && index !== active ? " is-inactive" : ""}`} role={tabbed ? "tabpanel" : undefined} aria-labelledby={tabbed ? `${id}-tab-${index}` : undefined} aria-hidden={tabbed && index !== active ? true : undefined} inert={tabbed && index !== active ? true : undefined}>
      {option.content}
    </div>)}
  </div>;
}
