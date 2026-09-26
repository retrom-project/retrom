export function gamepadCursorDirection(position, targetX, targetY) {
  return {
    x: position ? axis(targetX - position.x) : 1,
    y: position ? axis(targetY - position.y) : -1,
  };
}

function axis(distance) {return Math.abs(distance) <= 0.015 ? 0 : Math.sign(distance);}
