import {EventEmitter} from "node:events";
export const blockPolicy = Object.freeze({smallFileThresholdBytes: 0, networkWindowBytes: 262144});
// This transport fixture explicitly emits the BOOT observation at installation.
// Production observers never fall back to this test configuration.
export function policyContext(policy = blockPolicy) {
  const context = new EventEmitter();
  context.exposeBinding = async (_name, callback) => {context.recordPolicy = callback;};
  context.addInitScript = async () => {await context.recordPolicy({}, policy);};
  return context;
}
