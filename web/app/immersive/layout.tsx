import type { ReactNode } from "react";
import { ImmersiveAudioProvider } from "@/features/immersive/immersive-audio-provider";

export default function ImmersiveLayout({ children }: { children: ReactNode }) {
  return <ImmersiveAudioProvider>{children}</ImmersiveAudioProvider>;
}
