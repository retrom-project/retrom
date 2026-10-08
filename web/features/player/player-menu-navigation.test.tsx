import { useState } from "react";
import { act, render, screen } from "@testing-library/react";
import { expect, it } from "vitest";
import { navigatePlayerMenu } from "./player-menu-navigation";

it("controller volume changes reach a controlled React input", () => {
  render(<ControlledVolume />);
  const volume = screen.getByRole("slider", { name: "音量" });
  volume.focus();
  act(() => navigatePlayerMenu("left"));
  expect(volume).toHaveValue("45");
  expect(screen.getByRole("status")).toHaveTextContent("45");
  act(() => navigatePlayerMenu("right"));
  expect(volume).toHaveValue("50");
  expect(screen.getByRole("status")).toHaveTextContent("50");
});

function ControlledVolume() {
  const [volume, setVolume] = useState(50);
  return (
    <div className="player-menu">
      <input
        aria-label="音量"
        type="range"
        value={volume}
        onChange={(event) => setVolume(Number(event.target.value))}
      />
      <output role="status">{volume}</output>
    </div>
  );
}
