import { digestHex, EJSNetplayFrameBridge } from "./netplay/ejs-netplay-4.2.3-v1";
import { NetplayController } from "./netplay/controller";
import { reducePlayerOrientation } from "./orientation";
import type { MountedContext } from "./player-bootstrap";

type SyncTone = "synced" | "busy" | "warning";

export function startNetplay(context: MountedContext) {
  const { params, config, controller, resources } = context;
  if (!params.emulator.current || !config.netplay) {throw new Error("PLAYER_NETPLAY_CONFIG_INVALID");}
  try {
    params.netplayController.current?.dispose();
    const holder: { current?: NetplayController } = {};
    const current = () => !controller.signal.aborted && params.netplayController.current === holder.current;
    const created = new NetplayController(
      config.netplay, "", new EJSNetplayFrameBridge(params.emulator.current), netplayCallbacks(context, current),
    );
    holder.current = created;
    resources.ownedNetplayController = created;
    params.netplayController.current = created;
    params.setMessage("正在建立联机同步屏障…");
    void digestHex(new TextEncoder().encode(JSON.stringify(config.netplay.netplayProfile)))
      .then((digest) => created.setProfileDigest(digest))
      .then(() => created.start())
      .catch((caught: unknown) => failNetplayStart(context, created, current, caught));
    return true;
  } catch (caught) {
    params.setState("error");
    params.setMessage(caught instanceof Error ? caught.message : "NETPLAY_START_FAILED");
    return false;
  }
}

function netplayCallbacks(context: MountedContext, current: () => boolean) {
  const { params } = context;
  return {
    onStatus: (text: string, tone: SyncTone) => {
      if (current()) {params.setSyncText(text); params.setSyncTone(tone);}
    },
    onRunning: () => {
      if (!current()) {return;}
      params.setNetplayPaused(false);
      params.netplayPausedRef.current = false;
      const orientation = reducePlayerOrientation(
        params.orientationStateRef.current, { type: "runtime-started", paused: false },
      );
      params.orientationStateRef.current = orientation.state;
      params.setOrientationState(orientation.state);
      if (params.started.current) {return;}
      void params.sendEvent("start").then(() => {
        params.setState("running");
        params.heartbeat.current = window.setInterval(() => {void params.sendEvent("heartbeat");}, 30_000);
      }).catch(() => {
        params.setState("error");
        params.setMessage("PLAY_SESSION_EVENT_FAILED");
      });
    },
    onPaused: () => {
      if (current()) {params.netplayPausedRef.current = true; params.setNetplayPaused(true);}
    },
    onEnded: (reason: string) => {
      if (!current()) {return;}
      params.setSyncText("联机已结束");
      params.setSyncTone("warning");
      params.setMessage(reason);
      void params.sendEvent("finish").catch(() => undefined)
        .finally(() => window.setTimeout(() => window.location.replace(params.returnTo.current), 600));
    },
  };
}

function failNetplayStart(
  context: MountedContext,
  created: NetplayController,
  current: () => boolean,
  caught: unknown,
) {
  if (!current()) {created.dispose(); return;}
  created.end();
  context.params.setState("error");
  context.params.setMessage(caught instanceof Error ? caught.message : "NETPLAY_START_FAILED");
}
