import {useEffect, useRef, useState, type CSSProperties} from "react";

export function useLaunchControlWidth(hasSave: boolean) {
  const actionsRef = useRef<HTMLDivElement>(null);
  const [width, setWidth] = useState<number>();
  useEffect(() => {
    const actions = actionsRef.current;
    if (!actions) {return;}
    const measure = () => {
      const buttons = actions.querySelectorAll("button");
      const last = buttons[buttons.length - 1];
      if (!last) {return;}
      const next = last.getBoundingClientRect().right - actions.getBoundingClientRect().left;
      if (next > 0) {setWidth(next);}
    };
    measure();
    const observer = typeof ResizeObserver === "undefined" ? null : new ResizeObserver(measure);
    observer?.observe(actions);
    actions.querySelectorAll("button").forEach(button => observer?.observe(button));
    window.addEventListener("resize", measure);
    return () => {observer?.disconnect(); window.removeEventListener("resize", measure);};
  }, [hasSave]);
  const controlStyle: (CSSProperties & {"--launch-control-width": string}) | undefined = width ? {"--launch-control-width": `${width}px`} : undefined;
  return {actionsRef, controlStyle, width};
}
