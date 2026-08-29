import { existsSync, readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";
import { rpgRuntimeRoutes, validateRpgRuntimeConfig, type RpgGeneration, type RpgRuntimeConfig } from ".";
import { rpgValidationGates } from "../rpg-validation-protocol";

const launchId = "0198abcd-1234-7123-8abc-1234567890ab";
type Route = typeof rpgRuntimeRoutes[number];

function nativeConfig(origin: string) {
  return {
    runtimeFamily: "RPGMAKER",
    protocolVersion: 1,
    mode: "single",
    purpose: "RPG_RUNTIME_VALIDATION",
    launchId,
    coreId: "rpgmaker_mv",
    coreName: "RPG Maker MV",
    gameTitle: "Fixture",
    platformName: "RPG Maker",
    returnTo: "/admin/reviews/item",
    warnings: [],
    generation: "RPGMV",
    routeKey: "RPGMV_NATIVE",
    artifactId: "0198abcd-1234-7123-8abc-1234567890ac",
    checkpoint: null,
    checkpointAvailability: { available: false, reason: "RUNTIME_NOT_READY" },
    runtimeValidation: emptyValidationResume(),
    adapter: {
      adapterKind: "NATIVE_WEB",
      adapterId: "native-web",
      bridgeProfile: "RPGMV",
      uniqueOrigin: origin,
      bootstrapUrl: `${origin}/__retrom/bootstrap`,
      bootstrapTicket: "a".repeat(43),
    },
  } satisfies RpgRuntimeConfig;
}

function emptyValidationResume(): NonNullable<RpgRuntimeConfig["runtimeValidation"]> {
  return {
    validationId: "0198abcd-1234-7123-8abc-1234567890ad",
    state: "STARTING",
    originalLaunchId: launchId,
    restoreLaunchId: null,
    lastGateSequence: 0,
    machineGates: rpgValidationGates.map((gate) => ({
      gate, status: "NOT_STARTED", begunAtMs: null, completedAtMs: null, evidence: null, failureCode: null,
    })),
    checkpointEvidence: null,
    restoreScreenshotUploaded: false,
  };
}

describe("validateRpgRuntimeConfig native origin", () => {
  it("accepts HTTPS and the documented per-Launch localhost test origin", () => {
    expect(() => validateRpgRuntimeConfig(nativeConfig(`https://${launchId}.runtime.example.test`)))
      .not.toThrow();
    expect(() => validateRpgRuntimeConfig(nativeConfig(`http://${launchId}.rpg.localhost:8080`)))
      .not.toThrow();
  });

  it("rejects insecure non-localhost and a different localhost Launch", () => {
    expect(() => validateRpgRuntimeConfig(nativeConfig(`http://${launchId}.runtime.example.test:8080`)))
      .toThrow("PLAYER_RPG_CONFIG_INVALID");
    expect(() => validateRpgRuntimeConfig(nativeConfig("http://0198abcd-1234-7123-8abc-1234567890ac.rpg.localhost:8080")))
      .toThrow("PLAYER_RPG_CONFIG_INVALID");
  });

  it("rejects unknown config fields and malformed nested values with the stable decoder error", () => {
    expect(() => validateRpgRuntimeConfig({ ...nativeConfig(`https://${launchId}.runtime.example.test`), unknown: true } as RpgRuntimeConfig))
      .toThrow("PLAYER_RPG_CONFIG_INVALID");
    const missingAdapter = { ...nativeConfig(`https://${launchId}.runtime.example.test`), adapter: undefined } as unknown as RpgRuntimeConfig;
    expect(() => validateRpgRuntimeConfig(missingAdapter)).toThrow("PLAYER_RPG_CONFIG_INVALID");
    const sequenceMismatch = nativeConfig(`https://${launchId}.runtime.example.test`);
    sequenceMismatch.runtimeValidation!.lastGateSequence = 1;
    expect(() => validateRpgRuntimeConfig(sequenceMismatch)).toThrow("PLAYER_RPG_CONFIG_INVALID");
    const reordered = nativeConfig(`https://${launchId}.runtime.example.test`);
    reordered.runtimeValidation!.machineGates.reverse();
    expect(() => validateRpgRuntimeConfig(reordered)).toThrow("PLAYER_RPG_CONFIG_INVALID");
  });
});

describe("RPG runtime registry", () => {
  it("matches every pinned manifest route and points to a real frontend implementation", () => {
    const manifest = readJson<{ artifacts: ManifestRoute[] }>("../data/dat/rpgmaker/v1/manifest.json");
    const registry = readJson<{ schemaVersion: number; routes: FrontendRoute[] }>("features/player/rpg-runtime/registry.json");
    expect(registry.schemaVersion).toBe(1);
    expect(registry.routes).toHaveLength(7);
    const expected = manifest.artifacts.filter((route) => route.runtime_family === "RPGMAKER").map((route) => ({
      adapterId: route.adapter_id,
      adapterKind: route.runtime_adapter_kind,
      coreId: route.core_id,
      generation: route.generation,
      routeKey: route.route_key,
    })).sort(compareRoute);
    const registered = registry.routes.map((route) => ({
      adapterId: route.adapterId, adapterKind: route.adapterKind, coreId: route.coreId,
      generation: route.generation, routeKey: route.routeKey,
    })).sort(compareRoute);
    expect(registered).toEqual(expected);
    expect(rpgRuntimeRoutes.map((route) => ({
      adapterId: route.adapterId, adapterKind: route.adapterKind, coreId: route.coreId,
      generation: route.generation, routeKey: route.routeKey,
    })).sort(compareRoute)).toEqual(expected);
    for (const route of registry.routes) {
      expect(existsSync(resolve(process.cwd(), "features/player/rpg-runtime", route.implementation))).toBe(true);
    }
  });

  it("accepts the seven current rows and rejects every cross-generation combination", () => {
    const configurations = rpgRuntimeRoutes.map((route) => configFor(route));
    for (const configuration of configurations) {
      expect(() => validateRpgRuntimeConfig(configuration)).not.toThrow();
    }
    for (const configuration of configurations) {
      for (const generation of generations) {
        if (generation === configuration.generation) {continue;}
        expect(() => validateRpgRuntimeConfig({ ...configuration, generation }))
          .toThrow("PLAYER_RPG_CONFIG_INVALID");
      }
    }
  });

  it("requires mkxp project and RTP archives to be strict seekable blobs", () => {
    const route = rpgRuntimeRoutes.find((entry) => entry.generation === "RPGXP");
    if (!route) {throw new Error("missing RPGXP route");}
    const config = configFor(route);
    if (config.adapter.adapterKind !== "MKXP_LIBRETRO_WEB") {throw new Error("wrong RPGXP adapter");}

    config.adapter.projectArchive.rangeRequired = false as true;
    expect(() => validateRpgRuntimeConfig(config)).toThrow("PLAYER_RPG_CONFIG_INVALID");

    config.adapter.projectArchive.rangeRequired = true;
    config.adapter.projectArchive.kind = "FILE_TREE_V1" as "SEEKABLE_BLOB_V1";
    expect(() => validateRpgRuntimeConfig(config)).toThrow("PLAYER_RPG_CONFIG_INVALID");
  });

  it("accepts only the current Launch RTP file-tree index for EasyRPG", () => {
    const route = rpgRuntimeRoutes.find((entry) => entry.generation === "RPG2000");
    if (!route) {throw new Error("missing RPG2000 route");}
    const config = configFor(route);
    if (config.adapter.adapterKind !== "EASYRPG_WEB") {throw new Error("wrong RPG2000 adapter");}
    config.adapter.rtpSource = {
      kind: "FILE_TREE_V1",
      indexUrl: `/runtime/content/project/${"d".repeat(64)}/__retrom__/packs/0/index.json`,
    };
    expect(() => validateRpgRuntimeConfig(config)).not.toThrow();

    config.adapter.rtpSource.indexUrl = `/runtime/content/project/${"d".repeat(64)}/index.json`;
    expect(() => validateRpgRuntimeConfig(config)).toThrow("PLAYER_RPG_CONFIG_INVALID");
  });
});

type ManifestRoute = {
  adapter_id: string;
  core_id: string;
  generation: string;
  route_key: string;
  runtime_adapter_kind: string;
  runtime_family: string;
};

type FrontendRoute = {
  adapterId: string;
  adapterKind: string;
  coreId: string;
  generation: string;
  implementation: string;
  routeKey: string;
};

const generations: RpgGeneration[] = ["RPG2000", "RPG2003", "RPGXP", "RPGVX", "RPGVXACE", "RPGMV", "RPGMZ"];

function configFor(route: Route): RpgRuntimeConfig {
  const common: Omit<RpgRuntimeConfig, "adapter"> = {
    runtimeFamily: "RPGMAKER", protocolVersion: 1, mode: "single", purpose: "PRODUCT", launchId,
    coreId: route.coreId, coreName: `RPG Maker ${route.generation}`, gameTitle: "Fixture",
    platformName: "RPG Maker", returnTo: "/games/fixture", warnings: [], generation: route.generation,
    routeKey: route.routeKey, artifactId: "0198abcd-1234-7123-8abc-1234567890ac", checkpoint: null,
    checkpointAvailability: { available: false, reason: "RUNTIME_NOT_READY" },
    runtimeValidation: null,
  };
  const root = `/runtime/content/project/${"d".repeat(64)}/`;
  if (route.adapterKind === "EASYRPG_WEB") {
    const runtime = `/runtime/retrom-runtime/${route.runtimeVersion}/`;
    return { ...common, adapter: {
      adapterKind: "EASYRPG_WEB", adapterId: "easyrpg-web", engineMode: route.engineMode,
      runtimeBaseUrl: runtime, projectRootUrl: root,
      projectIndexUrl: `${root}index.json`, rtpSource: null, checkpointSlot: 100,
    }};
  }
  if (route.adapterKind === "MKXP_LIBRETRO_WEB") {
    const runtime = `/runtime/retrom-runtime/${route.runtimeVersion}/`;
    return { ...common, adapter: {
      adapterKind: "MKXP_LIBRETRO_WEB", adapterId: route.adapterId,
      core: {
        jsUrl: `${runtime}mkxp-z_libretro.js`,
        jsSizeBytes: 258192, jsSha256: "c".repeat(64),
        wasmUrl: `${runtime}mkxp-z_libretro.wasm`,
        wasmSizeBytes: 42487229, wasmSha256: "d".repeat(64),
        artifactSetSha256: "a".repeat(64),
      },
      runtimeBaseUrl: runtime,
      projectArchive: {
        kind: "SEEKABLE_BLOB_V1", rangeRequired: true,
        url: `${root}__retrom__/game.mkxpz`, sha256: "b".repeat(64), sizeBytes: 1,
      },
      rtpArchives: [], rgssVersion: route.rgssVersion, stateBufferBytes: 268435456,
    }};
  }
  const origin = `https://${launchId}.runtime.example.test`;
  return { ...common, adapter: {
    adapterKind: "NATIVE_WEB", adapterId: route.adapterId, bridgeProfile: route.bridgeProfile,
    uniqueOrigin: origin, bootstrapUrl: `${origin}/__retrom/bootstrap`, bootstrapTicket: "a".repeat(43),
  }};
}

function readJson<T>(path: string): T {
  return JSON.parse(readFileSync(resolve(process.cwd(), path), "utf8")) as T;
}

function compareRoute(left: { coreId: string }, right: { coreId: string }) {
  return left.coreId.localeCompare(right.coreId);
}
