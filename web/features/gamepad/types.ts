export type ControllerButton = Readonly<{
  pressed: boolean;
  touched: boolean;
  value: number;
}>;

export type ControllerSnapshot = Readonly<{
  index: number;
  connected: boolean;
  mapping: string;
  timestamp: number;
  buttons: readonly ControllerButton[];
  axes: readonly number[];
}>;

export type ControllerSource = Readonly<{
  read: () => readonly (ControllerSnapshot | null)[];
}>;

export type ControllerDirection = "up" | "down" | "left" | "right";

export type ControllerAction =
  | Readonly<{ type: "claimed"; index: number; centerButtonObservable: boolean }>
  | Readonly<{ type: "disconnected"; index: number }>
  | Readonly<{ type: "ready"; index: number }>
  | Readonly<{ type: "direction"; direction: ControllerDirection }>
  | Readonly<{ type: "confirm" }>
  | Readonly<{ type: "back" }>
  | Readonly<{ type: "previous-group" }>
  | Readonly<{ type: "next-group" }>
  | Readonly<{ type: "navigation" }>;
