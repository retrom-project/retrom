import { describe, expect, it } from "vitest";
import { chromeDevToolsWebVitalsGuardSource } from "./chrome-devtools-web-vitals-guard";

const reportAllChangesStack = `TypeError: Cannot read properties of undefined (reading 'startTime')
    at et.reportAllChanges (<anonymous>:2:19429)
    at <anonymous>:2:13070
    at <anonymous>:2:331
    at d (<anonymous>:2:6141)
    at <anonymous>:2:6326
    at <anonymous>:2:2895
    at n.timeout (<anonymous>:2:5652)`;

function installGuard() {
  window.eval(chromeDevToolsWebVitalsGuardSource);
}

function webVitalsErrorEvent(stack = reportAllChangesStack) {
  const error = new TypeError("Cannot read properties of undefined (reading 'startTime')");
  Object.defineProperty(error, "stack", { value: stack });
  return new ErrorEvent("error", {
    cancelable: true,
    error,
    message: "Uncaught TypeError: Cannot read properties of undefined (reading 'startTime')",
  });
}

describe("Chrome DevTools web-vitals guard", () => {
  it("quarantines only the injected reportAllChanges startTime failure before later listeners", () => {
    installGuard();
    let observed = false;
    window.addEventListener("error", () => { observed = true; }, { once: true });

    const accepted = window.dispatchEvent(webVitalsErrorEvent());

    expect(accepted).toBe(false);
    expect(observed).toBe(false);
  });

  it.each([
    ["application startTime error", "    at reportAllChanges (http://localhost:3000/app.js:2:19429)\n    at timeout (http://localhost:3000/app.js:2:5652)"],
    ["different anonymous error", "    at render (<anonymous>:2:19429)\n    at n.timeout (<anonymous>:2:5652)"],
  ])("does not suppress %s", (_label, stack) => {
    installGuard();
    let observed = false;
    window.addEventListener("error", () => { observed = true; }, { once: true });

    const accepted = window.dispatchEvent(webVitalsErrorEvent(stack));

    expect(accepted).toBe(true);
    expect(observed).toBe(true);
  });
});
