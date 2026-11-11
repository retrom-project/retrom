import "@testing-library/jest-dom/vitest";

// jsdom has no layout engine or ResizeObserver; components observing size receive no resize notifications in unit tests.
if (typeof globalThis.ResizeObserver === "undefined") {
  globalThis.ResizeObserver = class {
    observe() {}
    unobserve() {}
    disconnect() {}
  };
}
