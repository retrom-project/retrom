import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, it, vi } from "vitest";
import { SourceImportDrawer } from "./source-import-manager";

vi.mock("next/navigation", () => ({ useRouter: () => ({ push: vi.fn(), refresh: vi.fn() }) }));
afterEach(() => {cleanup(); vi.unstubAllGlobals();});

function setup() {
  const requests: Request[] = [];
  vi.stubGlobal("fetch", vi.fn(async (request: Request) => {
    requests.push(request);
    if (request.method === "POST") {return new Response("{}", { status: 400 });}
    return new Response(JSON.stringify({ rootId: "filesystem", path: "", items: [], nextCursor: null }), { status: 200 });
  }));
  render(<SourceImportDrawer open roots={[{ id: "filesystem", label: "服务器文件系统", status: "AVAILABLE" }]}
    platformInstances={[]} onClose={vi.fn()} onStarted={vi.fn()} />);
  return { user: userEvent.setup(), requests };
}

it("requires a format and enables extension filtering only for basic", async () => {
  const { user, requests } = setup();
  const format = screen.getByRole("combobox", { name: "文件组织格式" });
  const filter = screen.getByRole("textbox", { name: "扩展名筛选" });
  expect(format).toHaveValue("");
  expect(screen.getByRole("button", { name: "扫描此目录" })).toBeDisabled();
  expect(filter).toBeDisabled();
  expect(filter).toHaveAttribute("placeholder", "如 .nes;.zip；留空允许全部扩展名");
  expect(screen.queryByRole("radio")).not.toBeInTheDocument();
  expect(screen.getAllByRole("option").slice(1).map((option) => option.textContent))
    .toEqual(["basic", "metadata.pegasus.txt", "gamelist.xml"]);
  await user.selectOptions(format, "BASIC");
  expect(filter).toBeEnabled();
  expect(filter).toHaveAccessibleDescription(/以 . 开头，多个用 ; 分隔，留空允许全部文件/);
  await user.type(filter, ".NES;.zip");
  await user.click(screen.getByRole("button", { name: "扫描此目录" }));
  const posted = requests.find((request) => request.method === "POST");
  expect(await posted?.clone().json()).toEqual({ rootId: "filesystem", sourceRelativePath: "", format: "BASIC", extensionFilter: ".NES;.zip" });
  for (const value of ["PEGASUS", "GAMELIST"]) {
    await user.selectOptions(format, value);
    expect(filter).toBeDisabled();
  }
  await user.click(screen.getByRole("button", { name: "扫描此目录" }));
  expect(await requests.filter((request) => request.method === "POST").at(-1)?.clone().json())
    .toMatchObject({ format: "GAMELIST", extensionFilter: "" });
});

it("keeps invalid extensions out of the scan request", async () => {
  const { user, requests } = setup();
  await user.selectOptions(screen.getByRole("combobox", { name: "文件组织格式" }), "BASIC");
  await user.type(screen.getByRole("textbox", { name: "扩展名筛选" }), "*.nes");
  await user.click(screen.getByRole("button", { name: "扫描此目录" }));
  expect(await screen.findByText(/扩展名须以 . 开头/)).toBeVisible();
  expect(requests.some((request) => request.method === "POST")).toBe(false);
});
