"use client";

import { useRef } from "react";
import { useRouter } from "next/navigation";
import { AppIcon } from "@/components/app-icon";
import { useAuth } from "@/features/auth/auth-provider";
import { requestImmersiveFullscreen } from "@/features/immersive/immersive-fullscreen";

export function ImmersiveHomeEntry() {
  const { context } = useAuth();
  const router = useRouter();
  const navigatingRef = useRef(false);

  if (!context.user) {return null;}

  const enter = () => {
    if (navigatingRef.current) {return;}
    navigatingRef.current = true;
    void requestImmersiveFullscreen();
    router.push("/immersive");
  };

  return <button
    type="button"
    className="button secondary home-immersive-entry"
    aria-label="进入沉浸模式"
    onClick={enter}
  ><AppIcon name="gamepad" />沉浸模式</button>;
}
