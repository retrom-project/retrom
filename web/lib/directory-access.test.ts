import { afterEach, describe, expect, it, vi } from "vitest";
import { pickDirectory } from "./directory-access";

afterEach(() => vi.unstubAllGlobals());

function fileHandle(name: string, contents: string) {
  return { kind: "file" as const, name, getFile: async () => new File([contents], name) };
}

type TestFileHandle = ReturnType<typeof fileHandle>;
type TestDirectoryHandle = {
  kind: "directory";
  name: string;
  entries: () => AsyncGenerator<[string, TestDirectoryHandle | TestFileHandle]>;
};

function directoryHandle(name: string, children: Array<[string, TestDirectoryHandle | TestFileHandle]>): TestDirectoryHandle {
  return {
    kind: "directory" as const,
    name,
    async *entries() {
      for (const child of children) {yield child;}
    },
  };
}

describe("directory access", () => {
  it("recursively reads a directory handle and returns stable rooted relative paths", async () => {
    const root = directoryHandle("snes", [
      ["z.sfc", fileHandle("z.sfc", "z")],
      ["extras", directoryHandle("extras", [["a.sfc", fileHandle("a.sfc", "a")]])],
    ]);
    vi.stubGlobal("showDirectoryPicker", vi.fn(async () => root));

    const result = await pickDirectory();

    expect(result?.name).toBe("snes");
    expect(result?.files.map((entry) => entry.relativePath)).toEqual(["snes/extras/a.sfc", "snes/z.sfc"]);
    expect(await result?.files[0].file.text()).toBe("a");
  });

  it("treats cancelling the system directory selector as a no-op", async () => {
    vi.stubGlobal("showDirectoryPicker", vi.fn(async () => {throw new DOMException("cancelled", "AbortError");}));
    await expect(pickDirectory()).resolves.toBeNull();
  });

  it("reports unsupported browsers instead of falling back to an upload input", async () => {
    vi.stubGlobal("showDirectoryPicker", undefined);
    await expect(pickDirectory()).rejects.toThrow("当前浏览器不支持目录读取");
  });
});
