import type {RuntimeStartupKindV1, RuntimeStartupTaskV1} from "./contract";

export async function runHostStartup<T>(kind: RuntimeStartupKindV1,
  report: ((task: RuntimeStartupTaskV1) => void) | undefined, operation: () => Promise<T>, signal: AbortSignal): Promise<T> {
  const task: RuntimeStartupTaskV1 = {id: `host:${kind}`, kind, state: "RUNNING", progress: null};
  report?.(task);
  try {
    const value = await operation();
    if (!signal.aborted) {report?.({...task, state: "COMPLETED"});}
    return value;
  } catch (error) {
    if (!signal.aborted) {report?.({...task, state: "FAILED"});}
    throw error;
  }
}
