export async function requestValidatedLaunch(send, wait) {
  for (let attempt = 0; attempt < 60; attempt++) {
    const response = await send();
    if (response.status() === 202) {await wait(500); continue;}
    if (response.status() !== 201) {throw new Error(`SMOKE_LAUNCH_FAILED:${response.status()}`);}
    const created = await response.json();
    if (!created || typeof created.playUrl !== "string" || typeof created.launchId !== "string") {
      throw new Error("SMOKE_LAUNCH_INVALID");
    }
    return created;
  }
  throw new Error("SMOKE_VALIDATION_DEADLINE");
}
