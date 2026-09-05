import {sha256} from "@/lib/crypto";
import type {PlayerRuntimeV1, RuntimeStateV1} from "./contract";

type RuntimeE2ECheckpoint = {
  format: string;
  sha256: string;
  sizeBytes: number;
};

export type RuntimeE2EDiagnosticsV1 = {
  checkpoint(): Promise<RuntimeE2ECheckpoint>;
  getFrameCount(): number | null;
  getState(): RuntimeStateV1;
};

declare global {
  interface Window {
    __RETROM_E2E_RUNTIME_V1__?: RuntimeE2EDiagnosticsV1;
  }
}

export function installRuntimeE2EDiagnostics(
  runtime: PlayerRuntimeV1,
  environment: string | undefined = process.env.NODE_ENV,
) {
  if (environment === "production") {return () => undefined;}
  const diagnostics: RuntimeE2EDiagnosticsV1 = {
    getState: () => runtime.getState(),
    getFrameCount: () => runtime.getFrameCount(),
    checkpoint: async () => {
      const checkpoint = await runtime.checkpoint();
      const digest = await sha256(checkpoint.bytes);
      return {
        format: checkpoint.format,
        sha256: Array.from(digest, (value) => value.toString(16).padStart(2, "0")).join(""),
        sizeBytes: checkpoint.bytes.byteLength,
      };
    },
  };
  window.__RETROM_E2E_RUNTIME_V1__ = diagnostics;
  return () => {
    if (window.__RETROM_E2E_RUNTIME_V1__ === diagnostics) {delete window.__RETROM_E2E_RUNTIME_V1__;}
  };
}
