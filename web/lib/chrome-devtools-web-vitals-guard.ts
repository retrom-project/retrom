// Chrome DevTools currently injects a bundled web-vitals observer that can throw
// during soft-navigation metric resets. Keep this development-only guard exact:
// application errors, even ones mentioning startTime, must remain visible.
export const chromeDevToolsWebVitalsGuardSource = String.raw`(() => {
  const installationKey = "__retromChromeDevToolsWebVitalsGuardV1";
  if (window[installationKey] === true) return;
  Object.defineProperty(window, installationKey, { configurable: false, value: true });
  window.addEventListener("error", (event) => {
    const message = event.message;
    const targetMessage = "Cannot read properties of undefined (reading 'startTime')";
    const messageMatches = message === targetMessage || message === "TypeError: " + targetMessage ||
      message === "Uncaught TypeError: " + targetMessage;
    const stack = event.error && typeof event.error.stack === "string" ? event.error.stack : "";
    const injectedStack = stack.includes("reportAllChanges (<anonymous>:2:") &&
      stack.includes("n.timeout (<anonymous>:2:");
    if (!messageMatches || !injectedStack) return;
    event.preventDefault();
    event.stopImmediatePropagation();
  }, true);
})()`;
