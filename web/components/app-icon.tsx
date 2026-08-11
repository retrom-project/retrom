import type { ReactNode, SVGProps } from "react";

export type AppIconName = "home" | "library" | "heart" | "save" | "download" | "plus" | "clock" | "check" | "history" | "list" | "chip" | "database" | "folder" | "settings" | "arrow-left" | "search" | "pencil" | "x" | "grip" | "pause" | "play" | "eye-off" | "pin" | "gamepad" | "maximize" | "minimize" | "more" | "log-out" | "keyboard" | "warning";

const paths: Record<AppIconName, ReactNode> = {
  home: <><path d="m3 11 9-8 9 8" /><path d="M5 10v10h14V10M9 20v-6h6v6" /></>,
  library: <><rect x="3" y="4" width="18" height="16" rx="2" /><path d="M8 4v16M16 4v16M3 10h18" /></>,
  heart: <path d="M20.8 5.8a5.2 5.2 0 0 0-7.4 0L12 7.2l-1.4-1.4a5.2 5.2 0 1 0-7.4 7.4L12 22l8.8-8.8a5.2 5.2 0 0 0 0-7.4Z" />,
  save: <><path d="M5 4h12l2 2v14H5z" /><path d="M8 4v6h8V4M8 20v-6h8v6" /></>,
  download: <><path d="M12 3v12m-5-5 5 5 5-5" /><path d="M4 19h16" /></>,
  plus: <><path d="M12 5v14M5 12h14" /></>,
  clock: <><circle cx="12" cy="12" r="9" /><path d="M12 7v5l3 2" /></>,
  check: <><path d="m5 12 4 4L19 6" /></>,
  history: <><path d="M3 12a9 9 0 1 0 3-6.7L3 8" /><path d="M3 3v5h5M12 7v5l3 2" /></>,
  list: <><path d="M8 6h13M8 12h13M8 18h13" /><path d="M3 6h.01M3 12h.01M3 18h.01" /></>,
  chip: <><rect x="6" y="6" width="12" height="12" rx="2" /><path d="M9 1v5m6-5v5M9 18v5m6-5v5M1 9h5m-5 6h5m12-6h5m-5 6h5" /></>,
  database: <><ellipse cx="12" cy="5" rx="8" ry="3" /><path d="M4 5v7c0 1.7 3.6 3 8 3s8-1.3 8-3V5M4 12v7c0 1.7 3.6 3 8 3s8-1.3 8-3v-7" /></>,
  folder: <path d="M3 6.5h6l2 2h10v10.5a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2Z" />,
  settings: <><circle cx="12" cy="12" r="3" /><path d="M19.4 15a1.7 1.7 0 0 0 .3 1.9l.1.1-2.8 2.8-.1-.1a1.7 1.7 0 0 0-1.9-.3 1.7 1.7 0 0 0-1 1.6v.2h-4V21a1.7 1.7 0 0 0-1-1.6 1.7 1.7 0 0 0-1.9.3l-.1.1L4.2 17l.1-.1a1.7 1.7 0 0 0 .3-1.9A1.7 1.7 0 0 0 3 14H2.8v-4H3a1.7 1.7 0 0 0 1.6-1 1.7 1.7 0 0 0-.3-1.9L4.2 7 7 4.2l.1.1a1.7 1.7 0 0 0 1.9.3A1.7 1.7 0 0 0 10 3V2.8h4V3a1.7 1.7 0 0 0 1 1.6 1.7 1.7 0 0 0 1.9-.3l.1-.1L19.8 7l-.1.1a1.7 1.7 0 0 0-.3 1.9 1.7 1.7 0 0 0 1.6 1h.2v4H21a1.7 1.7 0 0 0-1.6 1Z" /></>,
  "arrow-left": <><path d="m15 18-6-6 6-6" /></>,
  search: <><circle cx="11" cy="11" r="7" /><path d="m20 20-4-4" /></>,
  pencil: <><path d="m4 20 4.2-1 10.5-10.5a2.1 2.1 0 0 0-3-3L5.2 16Z" /><path d="m14.5 6.7 2.8 2.8" /></>,
  x: <><path d="M6 6l12 12M18 6 6 18" /></>,
  grip: <><circle cx="9" cy="6" r="1" fill="currentColor" stroke="none" /><circle cx="15" cy="6" r="1" fill="currentColor" stroke="none" /><circle cx="9" cy="12" r="1" fill="currentColor" stroke="none" /><circle cx="15" cy="12" r="1" fill="currentColor" stroke="none" /><circle cx="9" cy="18" r="1" fill="currentColor" stroke="none" /><circle cx="15" cy="18" r="1" fill="currentColor" stroke="none" /></>,
  pause: <><rect x="6" y="5" width="4" height="14" rx="1" /><rect x="14" y="5" width="4" height="14" rx="1" /></>,
  play: <><path d="m8 5 11 7-11 7Z" /></>,
  "eye-off": <><path d="m3 3 18 18" /><path d="M10.6 10.6a2 2 0 0 0 2.8 2.8M9.9 4.2A10.8 10.8 0 0 1 12 4c5 0 9 5.2 9 8a8.5 8.5 0 0 1-2 3.6M6.6 6.6C4.3 8.1 3 10.4 3 12c0 2.8 4 8 9 8 1.2 0 2.4-.3 3.4-.8" /></>,
  pin: <><path d="m9 3 6 6" /><path d="m10 8-4.5 4.5 6 1 1 6L17 15" /><path d="m13 5 6 6" /><path d="M5 19 19 5" /></>,
  gamepad: <><path d="M8 8h8l4 4v5a3 3 0 0 1-5 2l-1-1h-4l-1 1a3 3 0 0 1-5-2v-5Z" /><path d="M8 13h4M10 11v4M16 12h.01M18 14h.01" /></>,
  maximize: <><path d="M8 3H3v5M16 3h5v5M21 16v5h-5M3 16v5h5" /><path d="m3 8 5-5m8 0 5 5m0 8-5 5M8 21l-5-5" /></>,
  minimize: <><path d="M8 8H3M8 8V3M16 8h5M16 8V3M16 16h5M16 16v5M8 16H3M8 16v5" /><path d="m3 8 5-5m8 0 5 5m0 8-5 5M8 21l-5-5" /></>,
  more: <><circle cx="5" cy="12" r="1.2" fill="currentColor" stroke="none" /><circle cx="12" cy="12" r="1.2" fill="currentColor" stroke="none" /><circle cx="19" cy="12" r="1.2" fill="currentColor" stroke="none" /></>,
  "log-out": <><path d="M10 4H5v16h5M14 8l4 4-4 4M9 12h9" /></>,
  keyboard: <><rect x="3" y="6" width="18" height="12" rx="2" /><path d="M7 10h.01M11 10h.01M15 10h.01M18 10h.01M7 14h.01M11 14h6" /></>,
  warning: <><path d="M12 3 2.8 20h18.4Z" /><path d="M12 9v4M12 17h.01" /></>
};

export function AppIcon({ name, ...props }: { name: AppIconName } & SVGProps<SVGSVGElement>) {
  return <svg aria-hidden="true" fill="none" stroke="currentColor" strokeLinecap="round" strokeLinejoin="round" strokeWidth="1.8" viewBox="0 0 24 24" {...props}>{paths[name]}</svg>;
}
