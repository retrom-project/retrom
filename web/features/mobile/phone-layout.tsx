"use client";

import { useSyncExternalStore, type ReactNode } from "react";

// Keep this query aligned with the phone-only rules in mobile-play.css.
export const phoneLayoutQuery = "(max-width: 767px), (pointer: coarse) and (max-width: 1023px) and (max-height: 767px)";

function subscribe(onChange: () => void) {
  if (typeof window.matchMedia !== "function") {return () => {};}
  const query = window.matchMedia(phoneLayoutQuery);
  query.addEventListener("change", onChange);
  return () => query.removeEventListener("change", onChange);
}

export function usePhoneLayout() {
  return useSyncExternalStore(subscribe, () => typeof window.matchMedia === "function" && window.matchMedia(phoneLayoutQuery).matches, () => false);
}

export function PhoneLayout({ phone, children }: { phone: ReactNode; children: ReactNode }) {
  return usePhoneLayout() ? phone : children;
}

export function PhoneDisclosure({ title, children }: { title: string; children: ReactNode }) {
  return usePhoneLayout()
    ? <details className="phone-disclosure"><summary>{title}</summary><div>{children}</div></details>
    : children;
}
