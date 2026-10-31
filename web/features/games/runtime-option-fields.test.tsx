import {cleanup, fireEvent, render, screen, within} from "@testing-library/react";
import {afterEach, expect, it, vi} from "vitest";
import {optionDescriptors, RuntimeOptionFields} from "./runtime-option-fields";

afterEach(cleanup);

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
