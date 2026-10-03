import type { ReactNode } from "react";
import { render as renderUI, renderHook as renderReactHook, type RenderOptions, type RenderHookOptions } from "@testing-library/react";
import { ToastProvider } from "./toast-provider";

export function render(ui: ReactNode, options?: RenderOptions) {
  return renderUI(ui, { wrapper: ToastProvider, ...options });
}

export function renderHook<Result, Props>(callback: (props: Props) => Result, options?: RenderHookOptions<Props>) {
  return renderReactHook(callback, { wrapper: ToastProvider, ...options });
}
