import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { StrictMode } from "react";
import { act, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, expect, it, vi } from "vitest";
import type {
  PlayerRuntimeV1,
  RuntimeEventV1,
  RuntimeStateV1,
} from "./runtime/contract";
import { loadProviderRuntime } from "./runtime/provider-dispatcher";
import { runFixture, runtimeFixture } from "./player-test-fixture";
import { usePlayerSession } from "./use-player-session";

vi.mock("./runtime/provider-dispatcher", () => ({
  loadProviderRuntime: vi.fn(),
}));
vi.mock("@/lib/api/client", () => ({
  api: { POST: vi.fn(async () => ({ data: {} })) },
}));
beforeEach(() => vi.clearAllMocks());

function runningRuntime() {
  let state: RuntimeStateV1 = "CREATED";
  const instance = runtimeFixture();
  instance.getState = () => state;
  instance.mount = vi.fn(async () => {
    state = "RUNNING";
  });
  instance.exit = vi.fn(async () => {
    state = "EXITED";
  });
  return instance;
}

function sessionRun() {
  const run = runFixture();
  run.envelope = JSON.parse(
    readFileSync(
      resolve(
        process.cwd(),
        "../api/runtime-provider/v1/fixtures/valid/checkpoint-restore.json",
      ),
      "utf8",
    ),
  ) as typeof run.envelope;
  return run;
}

type SessionProps = {
  run: ReturnType<typeof runFixture>;
  userId: string;
  onSnapshot: Parameters<typeof usePlayerSession>[4];
  onNativeChange?: Parameters<typeof usePlayerSession>[1];
  startAllowed?: boolean;
};
function Session({
  run,
  userId,
  onSnapshot,
  onNativeChange = () => undefined,
  startAllowed = true,
}: SessionProps) {
  const { mount, state, error, networkWaiting } = usePlayerSession(
    run,
    onNativeChange,
    () => undefined,
    null,
    onSnapshot,
    userId,
    "ON_DEMAND",
    async () => {},
    startAllowed,
  );
  return <div ref={mount}>{state}{error ? <p role="alert">{error}</p> : null}{networkWaiting ? <p>network-waiting</p> : null}</div>;
}

it.each([true, false])("shows a useful failure and never replaces native evidence with the rejected mount code: native=%s", async native => {
  let receive: (event: RuntimeEventV1) => void = () => undefined;
  const instance = runtimeFixture("FAILED");
  instance.subscribe = listener => {receive = listener; return () => undefined;};
  instance.mount = async () => {
    receive({type: "FATAL_ERROR", failure: {code: "PLAYER_CONTENT_REJECTED", category: "CONTENT", phase: "STARTUP", retryable: false,
      diagnostics: [
        ...(native ? [{source: "CORE" as const, message: "Required ROM program.bin is missing"}] : []),
        {source: "RUNTIME", message: "PLAYER_CONTENT_REJECTED"},
        {source: "RUNTIME", message: "at start ([URL])"},
      ]}});
    throw new Error("PLAYER_CONTENT_REJECTED");
  };
  vi.mocked(loadProviderRuntime).mockResolvedValue(instance);
  const view = render(<Session run={sessionRun()} userId="owner-a" onSnapshot={async () => undefined} />);
  const alert = await screen.findByRole("alert");
  expect(alert).toHaveTextContent("核心无法加载游戏文件，请检查游戏内容和运行配置。");
  if (native) {expect(alert).toHaveTextContent("program.bin");}
  expect(alert).not.toHaveTextContent("PLAYER_CONTENT_REJECTED");
  expect(alert).not.toHaveTextContent("at start");
  view.unmount();
});

it("waits for landscape before loading a provider and keeps a completed session across rotations", async () => {
  const instance = runningRuntime();
  vi.mocked(loadProviderRuntime).mockResolvedValue(instance);
  const props = {run: sessionRun(), userId: "owner-a", onSnapshot: vi.fn(async () => undefined)};
  const view = render(<Session {...props} startAllowed={false} />);
  await act(async () => {});
  expect(loadProviderRuntime).not.toHaveBeenCalled();
  view.rerender(<Session {...props} startAllowed />);
  await waitFor(() => expect(instance.mount).toHaveBeenCalledOnce());
  view.rerender(<Session {...props} startAllowed={false} />);
  view.rerender(<Session {...props} startAllowed />);
  expect(loadProviderRuntime).toHaveBeenCalledOnce();
  expect(instance.exit).not.toHaveBeenCalled();
  view.unmount();
});

it("cancels an unfinished start on portrait and never saves or mounts a late provider", async () => {
  const late = runningRuntime(), active = runningRuntime();
  let finish!: (value: PlayerRuntimeV1) => void;
  vi.mocked(loadProviderRuntime).mockImplementationOnce(() => new Promise(resolve => { finish = resolve; })).mockResolvedValueOnce(active);
  const props = {run: sessionRun(), userId: "owner-a", onSnapshot: vi.fn(async () => undefined)};
  const view = render(<Session {...props} startAllowed />);
  await waitFor(() => expect(loadProviderRuntime).toHaveBeenCalledOnce());
  view.rerender(<Session {...props} startAllowed={false} />);
  await act(async () => { finish(late); });
  expect(late.mount).not.toHaveBeenCalled();
  expect(late.exit).toHaveBeenCalledOnce();
  expect(props.onSnapshot).not.toHaveBeenCalled();
  view.rerender(<Session {...props} startAllowed />);
  await waitFor(() => expect(active.mount).toHaveBeenCalledOnce());
  view.unmount();
});

it("keeps the mounted runtime when callbacks and runtime events rerender the player", async () => {
  const instance = runningRuntime();
  let receive: (event: RuntimeEventV1) => void = () => undefined;
  instance.subscribe = (listener) => {
    receive = listener;
    return () => undefined;
  };
  vi.mocked(loadProviderRuntime).mockResolvedValue(instance);
  const run = sessionRun();
  const onSnapshot = vi.fn(async () => undefined);
  const view = render(
    <Session run={run} userId="owner-a" onSnapshot={onSnapshot} />,
  );
  await waitFor(() => expect(instance.mount).toHaveBeenCalledOnce());
  const newerCallback = vi.fn();
  view.rerender(
    <Session
      run={run}
      userId="owner-a"
      onSnapshot={onSnapshot}
      onNativeChange={newerCallback}
    />,
  );
  act(() =>
    receive({
      type: "CHECKPOINT_AVAILABILITY_CHANGED",
      availability: {
        available: true,
        reason: null,
        revision: "newer",
      },
    }),
  );
  expect(newerCallback).toHaveBeenCalledWith(instance);
  expect(loadProviderRuntime).toHaveBeenCalledOnce();
  expect(instance.exit).not.toHaveBeenCalled();
  view.unmount();
  await waitFor(() => expect(instance.exit).toHaveBeenCalledOnce());
});

it("disposes a late StrictMode provider without mounting over the active instance", async () => {
  const late = runningRuntime();
  const active = runningRuntime();
  let finish: (runtime: PlayerRuntimeV1) => void = () => undefined;
  vi.mocked(loadProviderRuntime)
    .mockImplementationOnce(
      () =>
        new Promise((resolveRuntime) => {
          finish = resolveRuntime;
        }),
    )
    .mockResolvedValueOnce(active);
  const view = render(
    <StrictMode>
      <Session
        run={sessionRun()}
        userId="owner-a"
        onSnapshot={async () => undefined}
      />
    </StrictMode>,
  );
  await waitFor(() => expect(active.mount).toHaveBeenCalledOnce());
  await act(async () => {
    finish(late);
  });
  expect(late.mount).not.toHaveBeenCalled();
  expect(late.exit).toHaveBeenCalledOnce();
  expect(active.getState()).toBe("RUNNING");
  view.unmount();
  await waitFor(() => expect(active.exit).toHaveBeenCalledOnce());
});

it("exports teardown through the original owner's callback before a new owner mounts", async () => {
  const first = runningRuntime();
  const next = runningRuntime();
  vi.mocked(loadProviderRuntime)
    .mockResolvedValueOnce(first)
    .mockResolvedValueOnce(next);
  const run = sessionRun();
  const originalOwner = vi.fn(async () => undefined);
  const otherOwner = vi.fn(async () => undefined);
  const view = render(
    <Session run={run} userId="owner-a" onSnapshot={originalOwner} />,
  );
  await waitFor(() => expect(first.mount).toHaveBeenCalledOnce());
  view.rerender(<Session run={run} userId="owner-b" onSnapshot={otherOwner} />);
  await waitFor(() => expect(next.mount).toHaveBeenCalledOnce());
  expect(originalOwner).toHaveBeenCalledOnce();
  expect(otherOwner).not.toHaveBeenCalled();
  expect(first.exit).toHaveBeenCalledOnce();
  view.unmount();
  await waitFor(() => expect(next.exit).toHaveBeenCalledOnce());
});

it("shows a network wait while content reads retry and clears it on recovery or failure", async () => {
  const instance = runningRuntime();
  let receive: (event: RuntimeEventV1) => void = () => undefined;
  instance.subscribe = listener => {receive = listener; return () => undefined;};
  vi.mocked(loadProviderRuntime).mockResolvedValue(instance);
  const view = render(<Session run={sessionRun()} userId="owner-a" onSnapshot={vi.fn(async () => undefined)} />);
  await waitFor(() => expect(instance.mount).toHaveBeenCalledOnce());
  act(() => receive({type: "CONTENT_NETWORK", state: "WAITING"}));
  expect(screen.getByText("network-waiting")).toBeInTheDocument();
  act(() => receive({type: "CONTENT_NETWORK", state: "RESTORED"}));
  expect(screen.queryByText("network-waiting")).toBeNull();
  act(() => receive({type: "CONTENT_NETWORK", state: "WAITING"}));
  act(() => receive({type: "FATAL_ERROR", failure: {code: "PLAYER_CONTENT_REJECTED", category: "NETWORK", phase: "PLAYING", retryable: false,
    diagnostics: []}}));
  expect(screen.queryByText("network-waiting")).toBeNull();
  expect(instance.exit).not.toHaveBeenCalled();
  view.unmount();
});
