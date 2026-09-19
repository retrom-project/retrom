"""Required managed Target coverage; logical input references carry no operator data."""
COMMON_SCENARIOS = {"preview-publish", "cold", "warm-new-launch", "save-restore-input", "exit", "performance-five-cold-warm"}
RANGE_SCENARIOS = {"concurrent-read", "metadata-no-body", "trace-10000", "read-exit", "fault-range-200", "fault-identity-412",
                   "fault-short-body", "worker-termination", "cache-corruption", "cache-denied"}
TARGET_MODES = {
    "neocd": "RANGE", "scummvm": "RANGE", "ppsspp": "RANGE", "play-ps2": "RANGE", "kirikiri2-kag": "RANGE",
    "rpgmaker-xp": "RANGE", "rpgmaker-vx": "RANGE", "rpgmaker-vx-ace": "RANGE",
    "flycast": "EAGER", "np2kai-pc98": "EAGER", "px68k": "EAGER", "gbe-pokemini": "EAGER", "openbor": "EAGER",
    "msx-webmsx": "EAGER", "flash-ruffle": "EAGER", "tic80": "EAGER", "fake08": "EAGER", "wasm4": "EAGER",
    "onscripter-yuri": "ON_OPEN", "butterscotch-gamemaker": "WORKSPACE", "nxengine": "PROJECT",
}
EXTRA_SCENARIOS = {
    "scummvm": {"multiple-files", "zero-file", "same-size-path-isolation"},
    "ppsspp": {"iso-and-cso", "strict-etag-and-length"},
    "play-ps2": {"device-done-error"},
    "kirikiri2-kag": {"xp3-and-zip", "main-jspi-and-pthread", "overlay-isolation"},
    "onscripter-yuri": {"unopened-file-no-body", "runtime-open-no-loading-overlay"},
    "butterscotch-gamemaker": {"workspace-denied", "workspace-corruption", "workspace-atomic-commit", "save-root-isolation"},
    "nxengine": {"required-file-set", "unrelated-file-no-body"},
}


def required_scenarios(target: str) -> set[str]:
    mode = TARGET_MODES[target]
    scenarios = COMMON_SCENARIOS | (RANGE_SCENARIOS if mode == "RANGE" else {"cache-denied", "size-boundaries"})
    if mode in {"EAGER", "WORKSPACE", "PROJECT"}:
        scenarios |= {"eager-progress"}
    if target in {"rpgmaker-xp", "rpgmaker-vx", "rpgmaker-vx-ace"}:
        scenarios |= {"retired-rtp-boundary", "native-thread-start-stop"}
    return scenarios | EXTRA_SCENARIOS.get(target, set())
