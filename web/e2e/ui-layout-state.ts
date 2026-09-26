import {execFileSync} from "node:child_process";
import path from "node:path";

export function uiLayoutState(action: "isolate" | "restore") {
  const database = process.env.RETROM_E2E_DATABASE;
  if (database) {
    execFileSync("python3", [path.resolve("../scripts/acceptance/ui_layout_state.py"), database, action]);
  }
}

export function seedHomeState(state: "empty" | "played" | "saved" | "populated") {
  const database = process.env.RETROM_E2E_DATABASE;
  if (database) {
    execFileSync("python3", [path.resolve("../scripts/acceptance/seed-ui-home.py"), database, state]);
  }
}
