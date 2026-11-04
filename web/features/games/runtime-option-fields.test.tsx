import {cleanup, fireEvent, render, screen, within} from "@testing-library/react";
import {afterEach, expect, it, vi} from "vitest";
import {optionDescriptors, RuntimeOptionFields} from "./runtime-option-fields";

afterEach(cleanup);

it("edits string arrays one item per line without changing order or filenames", () => {
  const fields = optionDescriptors({properties: {
    overlayArchives: {type: "array", maxItems: 32, items: {type: "string"}},
    unsupported: {type: "array", items: {type: "integer"}},
  }});
  const onChange = vi.fn();
  render(<RuntimeOptionFields fields={fields}
    options={{overlayArchives: ["patch z.xp3", "patch a.xp3"], engineMode: "KIRIKIRI2"}} onChange={onChange} />);
  const input = screen.getByRole("textbox", {name: "资源补丁归档（每行一个路径，后者优先）"});
  expect(input).toHaveValue("patch z.xp3\npatch a.xp3");
  expect(screen.queryByLabelText("unsupported")).toBeNull();
  fireEvent.change(input, {target: {value: "patch a.xp3\n\nsub/patch z.xp3\n"}});
  expect(input).toHaveValue("patch a.xp3\n\nsub/patch z.xp3\n");
  expect(onChange).toHaveBeenLastCalledWith({overlayArchives: ["patch a.xp3", "sub/patch z.xp3"], engineMode: "KIRIKIRI2"});
  fireEvent.change(input, {target: {value: ""}});
  expect(onChange).toHaveBeenLastCalledWith({engineMode: "KIRIKIRI2"});
});

it("presents engine and Steam choices in Chinese while preserving configuration values", () => {
  const fields = optionDescriptors({properties: {
    engineMode: {type: "string", enum: ["KIRIKIRI2", "KIRIKIRI_Z"]},
    steamService: {type: "string", enum: ["DISABLED", "LOCAL"]},
  }});
  const onChange = vi.fn();
  render(<RuntimeOptionFields fields={fields}
    options={{engineMode: "KIRIKIRI_Z", steamService: "DISABLED", overlayRoot: "locale"}} onChange={onChange} />);
  const engine = screen.getByRole("combobox", {name: "引擎模式"});
  expect(within(engine).getByRole("option", {name: "吉里吉里 2"})).toHaveValue("KIRIKIRI2");
  expect(within(engine).getByRole("option", {name: "吉里吉里 Z"})).toHaveValue("KIRIKIRI_Z");
  const steam = screen.getByRole("combobox", {name: "Steam 服务"});
  expect(within(steam).getByRole("option", {name: "禁用"})).toHaveValue("DISABLED");
  expect(within(steam).getByRole("option", {name: "本地兼容服务"})).toHaveValue("LOCAL");
  fireEvent.change(steam, {target: {value: "LOCAL"}});
  expect(onChange).toHaveBeenLastCalledWith({engineMode: "KIRIKIRI_Z", steamService: "LOCAL", overlayRoot: "locale"});
  fireEvent.change(engine, {target: {value: ""}});
  expect(onChange).toHaveBeenLastCalledWith({steamService: "DISABLED", overlayRoot: "locale"});
});
