export function runtimeFrameSources(template: string | undefined): string {
  const standard = "http://*.localhost:3000";
  // Local acceptance uses an isolated runtime origin on an explicitly allocated port.
  // Only add that configured localhost source; keep the remaining CSP unchanged.
  const local = template?.match(/^http:\/\/\{runId\}\.([a-z0-9-]+(?:\.[a-z0-9-]+)*\.localhost):(\d{1,5})$/u);
  if (!local || Number(local[2]) < 1 || Number(local[2]) > 65535) {return standard;}
  return `${standard} http://*.${local[1]}:${local[2]}`;
}
