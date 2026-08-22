import type { EJSNetplayFrameBridge } from "./ejs-netplay-4.2.3-v1";
import { coreStateBytes, digestHex } from "./ejs-netplay-4.2.3-v1";
import type { NetplayDiagnostics } from "./controller-model";
import { decodeStateFrame } from "./protocol";

export type PendingState = { transferId: string; nextFrame: number; byteLength: number; stateSha256: string; coreSha256: string };

export async function applyTransferredState(frame: Uint8Array, pending: PendingState, sessionId: string, epoch: number, bridge: EJSNetplayFrameBridge, diagnostics?: NetplayDiagnostics) {
  const decoded = decodeStateFrame(frame);
  const matches = decoded.sessionId === sessionId && decoded.transferId === pending.transferId && decoded.epoch === epoch && decoded.nextFrame === pending.nextFrame && decoded.state.byteLength === pending.byteLength;
  if (!matches) {throw new Error("PROTOCOL_VIOLATION");}
  const [stateSha256, coreSha256] = await Promise.all([digestHex(decoded.state), digestHex(coreStateBytes(decoded.state))]);
  if (stateSha256 !== pending.stateSha256 || coreSha256 !== pending.coreSha256) {throw new Error("STATE_INVALID");}
  const beforeDigest = await digestHex(coreStateBytes(bridge.captureState()));
  const loadResult = await bridge.loadStateForTransfer(decoded.state);
  const recapturedDigest = await digestHex(coreStateBytes(loadResult.recaptured));
  const coreExact = recapturedDigest === coreSha256;
  diagnostics?.onStateLoad?.({
    byteLength: decoded.state.byteLength, stateDigest: stateSha256, coreDigest: coreSha256,
    changed: beforeDigest !== recapturedDigest, nativeCompletion: true, byteExact: loadResult.byteExact, coreExact,
    expectedCoreBytes: loadResult.expectedCoreBytes, recapturedCoreBytes: loadResult.recapturedCoreBytes,
    firstCoreMismatch: loadResult.firstCoreMismatch,
  });
  if (!coreExact) {throw new Error("STATE_INVALID");}
  return { stateSha256, coreSha256 };
}
