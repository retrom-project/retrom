import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { useURLFilters } from "./use-url-filters";

vi.mock("next/navigation", async () => {
  const {useMockSearchParams} = await import("./url-filters.test-support");
  return {useSearchParams: useMockSearchParams};
});
afterEach(cleanup);
const parse = (params: URLSearchParams) => ({q:params.get("q") ?? "", sort:params.get("sort") ?? "DESC"});
const serialize = (value: ReturnType<typeof parse>) => new URLSearchParams(value).toString();
function Page() {
  const [filters, update] = useURLFilters(parse(new URLSearchParams(window.location.search)), parse, serialize);
  return <><input aria-label="搜索" value={filters.q} onChange={event => update(current => ({...current,q:event.target.value}))} />
    <output>{filters.sort}</output><button onClick={() => {
      update(current => ({...current, q:"new"})); update(current => ({...current,sort:"ASC"}));
    }}>更改两项</button></>;
}
it("restores navigation filters without writing old state over the URL", () => {
  window.history.replaceState({keep:true}, "", "/library?q=initial&sort=DESC");
  render(<Page />);
  fireEvent.change(screen.getByRole("textbox"), {target:{value:"draft"}});
  expect(window.location.search).toContain("q=draft");
  act(() => {window.history.replaceState({keep:true}, "", "/library?q=restored&sort=ASC");});
  expect(screen.getByRole("textbox")).toHaveValue("restored");
  expect(screen.getByText("ASC")).toBeVisible();
  expect(window.location.search).toBe("?q=restored&sort=ASC");
  expect(window.history.state).toEqual({keep:true});
  fireEvent.click(screen.getByRole("button", {name:"更改两项"}));
  expect(window.location.search).toBe("?q=new&sort=ASC");
  expect(screen.getByRole("textbox")).toHaveValue("new");
});
