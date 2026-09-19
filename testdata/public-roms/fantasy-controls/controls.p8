pico-8 cartridge // http://www.pico-8.com
version 42
__lua__
-- retrom fixture canonical-v1
x=20 c=8
function _update60()
 if btn(1) then x=(x+1)%100 end
 if btn(0) then x=(x+99)%100 end
 if btnp(4) then c=11 end
end
function _draw() cls(0) rectfill(x,20,x+4,24,c) end
