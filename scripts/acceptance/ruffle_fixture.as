// SPDX-License-Identifier: MIT
// Retrom-owned native SharedObject and standard-input product acceptance game.
package {
    import flash.display.Sprite;
    import flash.events.KeyboardEvent;
    import flash.net.SharedObject;
    public class RuffleFixture extends Sprite {
        private var position:int = 20;
        private var save:SharedObject;
        public function RuffleFixture() {
            save = SharedObject.getLocal("retrom-progress", "/");
            if (save.data.position !== undefined) position = int(save.data.position);
            stage.addEventListener(KeyboardEvent.KEY_DOWN, onKey);
            draw();
        }
        private function onKey(event:KeyboardEvent):void {
            if (event.keyCode == 37) position = Math.max(20, position - 20);
            if (event.keyCode == 39) position = Math.min(280, position + 20);
            if (event.keyCode == 27) position = 20;
            if (event.keyCode == 32) {
                save.data.position = position;
                save.flush();
            }
            draw();
        }
        private function draw():void {
            graphics.clear();
            graphics.beginFill(0x101020); graphics.drawRect(0, 0, 320, 240); graphics.endFill();
            graphics.beginFill(0xffffff); graphics.drawRect(position, 100, 10, 10); graphics.endFill();
        }
    }
}
