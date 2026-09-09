import assert from "node:assert/strict";
import {createHash} from "node:crypto";
import {execFileSync} from "node:child_process";
import {copyFileSync, mkdirSync, readFileSync} from "node:fs";
import {join, resolve} from "node:path";

const core = resolve(process.env.RETROM_RUFFLE_CORE_ROOT ?? "");
const imports = resolve(process.env.RETROM_RUFFLE_PLAYERGLOBAL ?? "");
const output = resolve(process.argv[2] ?? ".artifacts/ruffle/fixture");
assert.ok(process.env.RETROM_RUFFLE_CORE_ROOT && process.env.RETROM_RUFFLE_PLAYERGLOBAL && process.getuid() !== 0,
  "RUFFLE_FIXTURE_INPUT_REQUIRED");
const digest = (path) => createHash("sha256").update(readFileSync(path)).digest("hex");
assert.equal(digest(join(core, "tools/asc/asc.jar")), "4dc2aef13198cfa0692b2f2dda3cce10890277066d7c5aa2ac3e9c9f9a2c462e");
assert.equal(digest(imports), "0447e4f8fd58e3a2e00c48d45d0cdc0f19c5fc728de010af075b53a023d15c7d");
const image = "retrom-ruffle-toolchain:" + digest(join(core, ".github/rpg-runtime/Dockerfile")).slice(0, 16);
mkdirSync(output, {recursive: true});
copyFileSync(new URL("./ruffle_fixture.as", import.meta.url), join(output, "RuffleFixture.as"));
execFileSync("docker", ["run", "--rm", "--user", `${process.getuid()}:${process.getgid()}`,
  "--mount", `type=bind,src=${core},dst=/core,readonly`, "--mount", `type=bind,src=${imports},dst=/playerglobal.abc,readonly`,
  "--mount", `type=bind,src=${output},dst=/fixture`, image, "java", "-jar", "/core/tools/asc/asc.jar", "-AS3",
  "-import", "/playerglobal.abc", "-swf", "RuffleFixture,320,240,30", "/fixture/RuffleFixture.as"],
{stdio: "inherit", timeout: 30000});
const swf = join(output, "RuffleFixture.swf");
assert.equal(digest(swf), "1bbc6e9dbda46a9c9ac8860d6d97a8d8e96a56fe474ea9013a006d0a9f87cf8a", "RUFFLE_FIXTURE_BYTES_CHANGED");
console.log("ruffle fixture: deterministic bytes verified");
