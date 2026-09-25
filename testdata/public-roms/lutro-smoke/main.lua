-- Project-owned Lutro save fixture. The game writes progress only after B.
local game = lutro
local cursor = 0
local saved = 0
local wasRight = false
local wasConfirm = false

function game.conf(t)
  t.width = 320
  t.height = 240
end

function game.load()
  local progress = game.filesystem.read("progress.txt")
  saved = tonumber(progress) or 0
  cursor = saved
end

function game.update(dt)
  local right = game.input.joypad("right")
  local confirm = game.input.joypad("b")
  if right and not wasRight then cursor = math.min(cursor + 1, 8) end
  if confirm and not wasConfirm then
    saved = cursor
    assert(game.filesystem.write("progress.txt", tostring(saved)))
  end
  wasRight = right
  wasConfirm = confirm
end

function game.draw()
  local graphics = game.graphics
  graphics.setColor(20, 30, 50, 255)
  graphics.rectangle("fill", 0, 0, 320, 240)
  graphics.setColor(255, 200, 30, 255)
  graphics.rectangle("fill", 20 + 30 * cursor, 100, 20, 20)
  graphics.setColor(30, 220, 90, 255)
  graphics.rectangle("fill", 20 + 30 * saved, 150, 20, 20)
end
