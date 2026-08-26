#!/usr/bin/env python3
"""Deterministic unit checks for the RPG runtime source reproducer."""

from __future__ import annotations

import importlib.util
import json
import os
import subprocess
import sys
import tempfile
import unittest
import urllib.error
import zipfile
from pathlib import Path
from unittest.mock import patch


REPOSITORY_ROOT = Path(__file__).resolve().parents[1]
MODULE_PATH = REPOSITORY_ROOT / "data/dat/rpgmaker/v1/reproduce.py"
SPEC = importlib.util.spec_from_file_location("retrom_rpg_reproduce", MODULE_PATH)
if SPEC is None or SPEC.loader is None:
    raise RuntimeError("RPG_REPRODUCER_IMPORT_FAILED")
REPRODUCE = importlib.util.module_from_spec(SPEC)
sys.modules[SPEC.name] = REPRODUCE
sys.modules["reproduce"] = REPRODUCE
SPEC.loader.exec_module(REPRODUCE)
SOURCE_OFFER_PATH = MODULE_PATH.with_name("source_offer.py")
SOURCE_OFFER_SPEC = importlib.util.spec_from_file_location("retrom_rpg_source_offer", SOURCE_OFFER_PATH)
if SOURCE_OFFER_SPEC is None or SOURCE_OFFER_SPEC.loader is None:
    raise RuntimeError("RPG_SOURCE_OFFER_IMPORT_FAILED")
SOURCE_OFFER = importlib.util.module_from_spec(SOURCE_OFFER_SPEC)
sys.modules[SOURCE_OFFER_SPEC.name] = SOURCE_OFFER
SOURCE_OFFER_SPEC.loader.exec_module(SOURCE_OFFER)


class RPGMakerReproductionTests(unittest.TestCase):
    def test_retrom_rejects_local_runtime_builds(self) -> None:
        with self.assertRaisesRegex(
            REPRODUCE.ReproductionError,
            "RPG_RUNTIME_LOCAL_BUILD_UNSUPPORTED:use tagged fork release workflow",
        ):
            REPRODUCE.main(["--runtime", "mkxp"])

    def test_locked_input_download_retries_transient_http_failures_with_context(self) -> None:
        item = REPRODUCE.LockedInput(
            "fixture.tar.gz",
            1,
            "0" * 64,
            "https://example.invalid/fixture.tar.gz",
            "LICENSE",
            "RUNTIME_SOURCE",
        )
        failure = urllib.error.HTTPError(item.url, 502, "Bad Gateway", {}, None)
        with tempfile.TemporaryDirectory() as temporary:
            destination = Path(temporary) / item.filename
            with (
                patch.object(
                    REPRODUCE,
                    "download_input_once",
                    side_effect=[failure, failure, failure],
                ) as download,
                patch.object(REPRODUCE.time, "sleep") as sleep,
            ):
                with self.assertRaisesRegex(
                    REPRODUCE.ReproductionError,
                    "RPG_RUNTIME_BUILD_INPUT_DOWNLOAD_FAILED:fixture.tar.gz",
                ):
                    REPRODUCE.download_input(item, destination)
            self.assertEqual(3, download.call_count)
            self.assertEqual([((1,), {}), ((2,), {})], sleep.call_args_list)

    def test_locked_input_download_recovers_from_one_transient_http_failure(self) -> None:
        item = REPRODUCE.LockedInput(
            "fixture.tar.gz",
            1,
            "0" * 64,
            "https://example.invalid/fixture.tar.gz",
            "LICENSE",
            "RUNTIME_SOURCE",
        )
        failure = urllib.error.HTTPError(item.url, 503, "Unavailable", {}, None)
        with (
            patch.object(
                REPRODUCE,
                "download_input_once",
                side_effect=[failure, None],
            ) as download,
            patch.object(REPRODUCE.time, "sleep") as sleep,
        ):
            REPRODUCE.download_input(item, Path("fixture.tar.gz"))
        self.assertEqual(2, download.call_count)
        sleep.assert_called_once_with(1)

    def test_lock_is_complete_and_has_no_floating_or_pages_urls(self) -> None:
        items = REPRODUCE.load_lock()
        self.assertEqual(81, len(items))
        self.assertIn("easy-icudata.tar.gz", items)
        self.assertIn("mkxp-gcem.zip", items)
        self.assertIn("mkxp-egl-registry.tar.gz", items)
        self.assertIn("toolchain-wasi-sdk-30-llvm-project.tar.gz", items)
        for item in items.values():
            lowered = item.url.lower()
            self.assertNotIn("github.io", lowered)
            self.assertNotIn("/latest/", lowered)
            self.assertNotIn("lastsuccessfulbuild", lowered)

    def test_clean_output_comparison_fails_closed_on_one_byte_drift(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            left = root / "left"
            right = root / "right"
            left.mkdir()
            right.mkdir()
            for name in REPRODUCE.OUTPUT_NAMES["easyrpg"]:
                (left / name).write_bytes(b"same")
                (right / name).write_bytes(b"same")
            (right / "easyrpg-player.wasm").write_bytes(b"drift")
            with self.assertRaisesRegex(
                REPRODUCE.ReproductionError, "RPG_RUNTIME_BUILD_NOT_REPRODUCIBLE"
            ):
                REPRODUCE.compare_outputs("easyrpg", left, right)

    def test_clean_build_uses_fixed_image_and_disables_container_network(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            with patch.object(REPRODUCE.subprocess, "run") as run:
                _, context = REPRODUCE.run_clean_build(
                    "easyrpg", root / "source-cache", root
                )
                try:
                    command = run.call_args.args[0]
                    self.assertEqual("docker", command[0])
                    self.assertEqual("none", command[command.index("--network") + 1])
                    self.assertEqual(
                        "retrom-rpg-build", command[command.index("--hostname") + 1]
                    )
                    self.assertIn("--cidfile", command)
                    self.assertIn("--name", command)
                    self.assertIn(REPRODUCE.IMAGES["easyrpg"], command)
                    self.assertRegex(REPRODUCE.IMAGES["easyrpg"], r"@sha256:[0-9a-f]{64}$")
                finally:
                    context.cleanup()

    def test_clean_build_failures_clean_only_the_recorded_container(self) -> None:
        failure_kinds = ("called-process", "os-error", "keyboard-interrupt")
        for failure_kind in failure_kinds:
            with self.subTest(failure_kind=failure_kind):
                calls: list[list[str]] = []
                container_id = "a" * 64

                def run(command: list[str], **_kwargs: object) -> None:
                    calls.append(command)
                    if command[1] != "run":
                        return
                    Path(command[command.index("--cidfile") + 1]).write_text(
                        f"{container_id}\n", encoding="ascii"
                    )
                    if failure_kind == "called-process":
                        raise subprocess.CalledProcessError(1, command)
                    if failure_kind == "os-error":
                        raise OSError("docker unavailable")
                    raise KeyboardInterrupt

                expected = (
                    KeyboardInterrupt
                    if failure_kind == "keyboard-interrupt"
                    else REPRODUCE.ReproductionError
                )
                with tempfile.TemporaryDirectory() as temporary:
                    root = Path(temporary)
                    with patch.object(REPRODUCE.subprocess, "run", side_effect=run):
                        with self.assertRaises(expected):
                            REPRODUCE.run_clean_build(
                                "easyrpg", root / "source-cache", root
                            )

                self.assertEqual(
                    ["docker", "stop", "--time", "10", container_id], calls[1]
                )
                self.assertEqual(
                    ["docker", "rm", "--force", container_id], calls[2]
                )
                self.assertFalse(
                    any(command[1] in {"ps", "inspect"} for command in calls)
                )

    def test_invalid_container_id_never_reaches_docker_cleanup(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            cidfile = Path(temporary) / "container.cid"
            cidfile.write_text("not-a-container-id\n", encoding="ascii")
            with patch.object(REPRODUCE.subprocess, "run") as run:
                with self.assertRaisesRegex(
                    REPRODUCE.ReproductionError,
                    "RPG_RUNTIME_BUILD_CONTAINER_ID_INVALID",
                ):
                    REPRODUCE.cleanup_build_container(cidfile)
            run.assert_not_called()
            self.assertFalse(cidfile.exists())

    def test_source_offer_covers_every_locked_input_and_labels_binary_relationship(self) -> None:
        manifest = {
            "build": {
                "reproducible_outputs": {
                    "easyrpg": {"runtime_version": "0.8.1.1-v4"},
                    "mkxp": {"runtime_version": "f2efc98-v3"},
                }
            }
        }
        with tempfile.TemporaryDirectory() as temporary:
            locked = SOURCE_OFFER.locked_offer_inputs(Path(temporary))
        self.assertEqual(81, len(locked))
        self.assertEqual(9, len(SOURCE_OFFER.PRIMARY_ASSOCIATIONS))
        self.assertEqual(90, len(locked) + len(SOURCE_OFFER.PRIMARY_ASSOCIATIONS))
        for item in locked:
            self.assertIn(item.association, SOURCE_OFFER.OFFER_ASSOCIATIONS)
            self.assertTrue(SOURCE_OFFER.binary_targets(item, manifest))
            self.assertTrue(SOURCE_OFFER.binary_targets(item, {"build": {}}))
        self.assertEqual(
            "BUILD_PROCESS_ONLY",
            SOURCE_OFFER.binary_association(next(
                item for item in locked if item.filename == "mkxp-wasi-sdk.tar.gz"
            )),
        )
        self.assertEqual(
            "EXACT_REPRODUCIBLE_BUILD",
            SOURCE_OFFER.binary_association(next(
                item for item in locked if item.filename == "easy-icu.tar.gz"
            ), manifest),
        )
        self.assertEqual(
            "SOURCE_INPUT_NOT_REPRODUCED",
            SOURCE_OFFER.binary_association(next(
                item for item in locked if item.filename == "easy-icu.tar.gz"
            ), {"build": {}}),
        )
        tagged = json.loads(
            (MODULE_PATH.parent / "manifest.json").read_text(encoding="utf-8")
        )
        self.assertEqual(
            "TAGGED_RELEASE_COMPATIBLE",
            SOURCE_OFFER.binary_association(next(
                item for item in locked if item.filename == "easy-icu.tar.gz"
            ), tagged),
        )

    def test_deterministic_zip_drops_info_zip_environment_recursion(self) -> None:
        wrapper = (MODULE_PATH.parent / "builder/deterministic-zip.sh").read_text(
            encoding="utf-8"
        )
        self.assertIn("unset ZIP ZIPOPT\n", wrapper)
        self.assertIn("LC_ALL=C sort -z -u", wrapper)
        self.assertIn('/usr/bin/zip -X -@ "$ARCHIVE"\n', wrapper)

    def test_deterministic_zip_ignores_filesystem_creation_order(self) -> None:
        wrapper = MODULE_PATH.parent / "builder/deterministic-zip.sh"
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            archives: list[Path] = []
            for label, names in (
                ("ascending", [f"entry-{index:03d}" for index in range(64)]),
                ("descending", [f"entry-{index:03d}" for index in reversed(range(64))]),
            ):
                workspace = root / label
                tree = workspace / "tree"
                tree.mkdir(parents=True)
                for name in names:
                    directory = tree / name
                    directory.mkdir()
                    (directory / "payload.txt").write_text(
                        f"fixed:{name}\n", encoding="utf-8"
                    )
                archive = root / f"{label}.zip"
                environment = os.environ | {
                    "TMPDIR": str(root),
                    "ZIP": "must-not-reach-info-zip",
                    "ZIPOPT": "must-not-reach-info-zip",
                }
                subprocess.run(
                    [wrapper, "-r", archive, "tree"],
                    cwd=workspace,
                    env=environment,
                    check=True,
                    stdout=subprocess.DEVNULL,
                )
                archives.append(archive)

            self.assertEqual(archives[0].read_bytes(), archives[1].read_bytes())
            with zipfile.ZipFile(archives[0]) as bundle:
                self.assertEqual(sorted(bundle.namelist()), bundle.namelist())

    def test_mkxp_builder_applies_fixed_cmake_compatibility_patch(self) -> None:
        builder = (MODULE_PATH.parent / "builder/mkxp.sh").read_text(encoding="utf-8")
        compatibility = (
            MODULE_PATH.parent / "patches/mkxp-openal-cmake-compat.patch"
        ).read_text(encoding="utf-8")
        self.assertIn("prepare_subproject openal-soft", builder)
        self.assertIn(
            "patch -p1 < /recipe/patches/mkxp-openal-cmake-compat.patch", builder
        )
        self.assertIn("+        $<BUILD_INTERFACE:alsoft::fmt>)", compatibility)

    def test_mkxp_builder_applies_the_final_declared_wrap_patch(self) -> None:
        builder = (MODULE_PATH.parent / "builder/mkxp.sh").read_text(encoding="utf-8")
        self.assertIn("printf '%s\\n' \"$declaration\" | tr ',' '\\n'", builder)
        self.assertNotIn("printf '%s' \"$declaration\" | tr ',' '\\n'", builder)

    def test_mkxp_builder_pins_fluidsynth_gcem_submodule(self) -> None:
        builder = (MODULE_PATH.parent / "builder/mkxp.sh").read_text(encoding="utf-8")
        deterministic = (
            MODULE_PATH.parent / "patches/mkxp-deterministic-build.patch"
        ).read_text(encoding="utf-8")
        item = REPRODUCE.load_lock()["mkxp-gcem.zip"]
        self.assertEqual(
            "28159274c54e9640354852e172d10d88eb159f4e7f2fea42edbcd20105ed3526",
            item.sha256,
        )
        self.assertEqual("LICENSE;NOTICE.txt", item.license_spec)
        self.assertIn("gcem-012ae73c6d0a2cb09ffe86475f5c6fba3926e200", builder)
        self.assertIn("/work/mkxp/subprojects/fluidsynth/gcem", builder)
        self.assertIn("rmdir /work/mkxp/subprojects/fluidsynth/gcem", builder)
        self.assertIn(
            "test -f /work/mkxp/subprojects/fluidsynth/gcem/include/gcem.hpp",
            builder,
        )
        self.assertIn("'ALSOFT_INSTALL': false", deterministic)
        self.assertIn(
            "'GCEM_INCLUDE_DIR': '/work/mkxp/subprojects/fluidsynth/gcem/include'",
            deterministic,
        )

    def test_mkxp_retroarch_port_closure_is_exactly_locked_zlib(self) -> None:
        builder = (MODULE_PATH.parent / "builder/mkxp.sh").read_text(encoding="utf-8")
        item = REPRODUCE.load_lock()["mkxp-zlib.tar.gz"]
        self.assertEqual(1572744, item.size_bytes)
        self.assertEqual(
            "17e88863f3600672ab49182f217281b6fc4d3c762bde361935e436a95214d05c",
            item.sha256,
        )
        self.assertEqual("LICENSE", item.license_spec)
        self.assertIn("$'USE_SDL=2\\nUSE_ZLIB=1'", builder)
        self.assertIn("grep -Fx 'HAVE_SDL2 = 0'", builder)
        self.assertIn("grep -Fx 'HAVE_AL ?= 0'", builder)
        self.assertIn("HAVE_SDL2=0", builder)
        self.assertIn("embuilder build zlib", builder)
        self.assertIn(
            "8c9642495bafd6fad4ab9fb67f09b268c69ff9af0f4f20cf15dfc18852ff1f312"
            "bd8ca41de761b3f8d8e90e77d79f2ccacd3d4c5b19e475ecf09d021fdfe9088",
            builder,
        )

    def test_mkxp_theora_uses_the_locked_sibling_ogg_headers(self) -> None:
        deterministic = (
            MODULE_PATH.parent / "patches/mkxp-deterministic-build.patch"
        ).read_text(encoding="utf-8")
        self.assertIn(
            "+        include_directories: ['include', '../ogg/include']",
            deterministic,
        )
        self.assertNotIn("ogg-prefix", deterministic)

    def test_mkxp_builder_pins_nested_egl_registry_subproject(self) -> None:
        builder = (MODULE_PATH.parent / "builder/mkxp.sh").read_text(encoding="utf-8")
        item = REPRODUCE.load_lock()["mkxp-egl-registry.tar.gz"]
        self.assertEqual(
            "00f6d5656d8a075ee72ec2ccbe9b803d29bb56695b05622ac4cde95d760a1528",
            item.sha256,
        )
        self.assertEqual("PER_FILE_LICENSE_HEADERS", item.license_spec)
        self.assertIn("prepare_subproject egl-registry", builder)
        self.assertIn("EGL-Registry-3ae2b7c48690d2ce13cc6db3db02dfc0572be65e", builder)


if __name__ == "__main__":
    unittest.main()
