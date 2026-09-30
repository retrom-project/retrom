import {useEffect, useId, useRef, useState, type ReactNode} from "react";
import {LaunchContentLoading, LaunchContentLoadingSummary, launchLoadingCount} from "./launch-content-loading";
import {DOSProgramPicker} from "./dos-program-picker";
import type {CoreOption, DOSEntry} from "./launch-controls";

export function DesktopLaunchSettings({coreOptions, selectedCore, savedCoreId, isDOS, dosEntries, defaultDosEntry, dosEntry, onDOSChange, controlWidth}: {
  coreOptions: CoreOption[];
  selectedCore?: CoreOption;
  savedCoreId?: string;
  isDOS: boolean;
  dosEntries: DOSEntry[];
  defaultDosEntry: string | null;
  dosEntry: string | null;
  onDOSChange: (value: string | null) => void;
  controlWidth?: number;
}) {
  const savedCore = savedCoreId === undefined ? undefined : coreOptions.find(core => core.coreId === savedCoreId) ?? {};
  const program = dosEntries.find(entry => entry.path === dosEntry)?.originalPath;
  return <DesktopLaunchOptions controlWidth={controlWidth} loadingCount={launchLoadingCount(selectedCore, savedCore)}
    loading={<LaunchContentLoading selectedCore={selectedCore} savedCore={savedCore} />}
    loadingSummary={<LaunchContentLoadingSummary selectedCore={selectedCore} savedCore={savedCore} />}
    program={isDOS ? <DOSProgramPicker defaultDosEntry={defaultDosEntry} dosEntries={dosEntries} onChange={onDOSChange} value={dosEntry} /> : undefined}
    programSummary={<span title={program ?? "显示 DOSBox Pure 程序菜单"}>{program?.split(/[\\/]/).pop() ?? "程序菜单"}</span>} />;
}

function useCompactOptions(enabled: boolean, controlWidth: number | undefined) {
  const ref = useRef<HTMLDivElement>(null);
  const [compact, setCompact] = useState(false);
  useEffect(() => {
    const element = ref.current;
    if (!element) {return;}
    const measure = () => {
      const width = element.getBoundingClientRect().width;
      const gap = parseFloat(getComputedStyle(element).columnGap) || 12;
      if (width > 0) {setCompact(enabled && width < (controlWidth ?? 308) + gap + 280);}
    };
    measure();
    const observer = typeof ResizeObserver === "undefined" ? null : new ResizeObserver(measure);
    observer?.observe(element);
    window.addEventListener("resize", measure);
    return () => {observer?.disconnect(); window.removeEventListener("resize", measure);};
  }, [enabled, controlWidth]);
  return {ref, compact: enabled && compact};
}

export function DesktopLaunchOptions({loading, program, loadingSummary, programSummary, loadingCount, controlWidth}: {
  loading: ReactNode;
  program?: ReactNode;
  loadingSummary: ReactNode;
  programSummary: ReactNode;
  loadingCount: number;
  controlWidth?: number;
}) {
  const paired = loadingCount > 0 && Boolean(program);
  const {ref, compact} = useCompactOptions(paired, controlWidth);
  const [active, setActive] = useState(0);
  const id = useId();
  const tabsRef = useRef<HTMLDivElement>(null);
  const options = [
    {label: "内容加载", summary: loadingSummary, content: loading},
    {label: "启动程序", summary: programSummary, content: program},
  ];
  return <div ref={ref} className={`launch-options${paired ? " is-paired" : ""}${compact ? " is-tabbed" : ""}${loadingCount > 1 ? " has-separate-loading" : ""}`}>
    {compact ? <div ref={tabsRef} className="launch-option-tabs" role="tablist" aria-label="启动设置">
      {options.map((option, index) => <button key={option.label} type="button" className="button secondary option-tab" role="tab" id={`${id}-tab-${index}`} aria-controls={`${id}-panel-${index}`} aria-selected={index === active} tabIndex={index === active ? 0 : -1} onClick={() => setActive(index)} onKeyDown={event => {
        const next = event.key === "Home" ? 0 : event.key === "End" ? 1
          : event.key === "ArrowLeft" || event.key === "ArrowRight" ? 1 - index : -1;
        if (next < 0) {return;}
        event.preventDefault(); setActive(next);
        tabsRef.current?.querySelectorAll<HTMLButtonElement>("button")[next]?.focus();
      }}><span>{option.label}</span><small>{option.summary}</small></button>)}
    </div> : null}
    {options.map((option, index) => <div key={option.label} id={`${id}-panel-${index}`} className={`launch-option-panel${compact && index !== active ? " is-inactive" : ""}`} role={compact ? "tabpanel" : undefined} aria-labelledby={compact ? `${id}-tab-${index}` : undefined} aria-hidden={compact && index !== active ? true : undefined} inert={compact && index !== active ? true : undefined}>
      {option.content}
    </div>)}
  </div>;
}
