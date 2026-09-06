import {gameSaveInstructions, type CheckpointSemantics} from "./checkpoint-semantics";

export function CheckpointHelp({semantics, visible, retryAvailable, onRetry}: {
  semantics: CheckpointSemantics; visible: boolean; retryAvailable?: boolean; onRetry?: () => void;
}) {
  if (semantics !== "GAME_SAVE" || !visible) {return null;}
  return <div className="player-native-save-help"><p>{gameSaveInstructions}</p>
    {retryAvailable ? <button type="button" className="button" onClick={onRetry}>重试同步</button> : null}</div>;
}

export function NativeSaveToast({visible, semantics, toast, text, tone}: {
  visible: boolean; semantics: CheckpointSemantics | undefined; toast: string; text: string; tone: string;
}) {
  if (!visible || semantics !== "GAME_SAVE") {return null;}
  const message = tone !== "synced" ? text : toast;
  return <div className={`player-toast${message ? " is-visible" : ""}`} role="status" aria-live="polite">{message}</div>;
}
