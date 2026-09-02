export type PlayerRuntimeErrorCode =
  | "PLAYER_LAUNCH_ENVELOPE_INVALID"
  | "PLAYER_PROVIDER_MODULE_INVALID"
  | "PLAYER_PROVIDER_MODULE_DIGEST_INVALID"
  | "PLAYER_RUNTIME_DIAGNOSTIC_INVALID"
  | "PLAYER_RUNTIME_FRAME_INVALID"
  | "PLAYER_RUNTIME_CONTRACT_INVALID"
  | "PLAYER_RUNTIME_RESTORE_INVALID"
  | "PLAYER_RUNTIME_THREADS_UNAVAILABLE";

export class PlayerRuntimeError extends Error {
  readonly code: PlayerRuntimeErrorCode;

  constructor(code: PlayerRuntimeErrorCode) {
    super(code);
    this.name = "PlayerRuntimeError";
    this.code = code;
  }
}

export function playerRuntimeError(code: PlayerRuntimeErrorCode) {
  return new PlayerRuntimeError(code);
}
