/*:
 * @plugindesc Retrom-owned deterministic RPG Maker MV smoke scene.
 * @author Retrom
 */

(function() {
    "use strict";

    var fixtureMarker = "RETROM RPGMV";
    var markerName = "retrom-marker";
    var toneUrl = "audio/se/retrom-tone.wav";

    Scene_Boot.prototype.loadSystemWindowImage = function() {};
    Scene_Boot.loadSystemImages = function() {};
    Scene_Boot.prototype.isGameFontLoaded = function() { return true; };
    Scene_Boot.prototype.start = function() {
        Scene_Base.prototype.start.call(this);
        this.checkPlayerLocation();
        DataManager.setupNewGame();
        $gameSystem.disableMenu();
        document.title = fixtureMarker;
        SceneManager.goto(Scene_Map);
    };

    function makeBackground() {
        var bitmap = new Bitmap(Graphics.width, Graphics.height);
        bitmap.fillAll("#101827");
        for (var x = 0; x < Graphics.width; x += 48) {
            bitmap.fillRect(x, 0, 1, Graphics.height, "#25334a");
        }
        for (var y = 0; y < Graphics.height; y += 48) {
            bitmap.fillRect(0, y, Graphics.width, 1, "#25334a");
        }
        return new Sprite(bitmap);
    }

    function RetromPlayerSprite() {
        this.initialize.apply(this, arguments);
    }
    RetromPlayerSprite.prototype = Object.create(Sprite.prototype);
    RetromPlayerSprite.prototype.constructor = RetromPlayerSprite;
    RetromPlayerSprite.prototype.initialize = function() {
        Sprite.prototype.initialize.call(this, new Bitmap(30, 30));
        this.bitmap.fillRect(0, 0, 30, 30, "#40d0ff");
        this.bitmap.fillRect(5, 5, 20, 20, "#ffffff");
        this.anchor.x = 0.5;
        this.anchor.y = 1;
    };
    RetromPlayerSprite.prototype.update = function() {
        Sprite.prototype.update.call(this);
        this.x = $gamePlayer.screenX();
        this.y = $gamePlayer.screenY();
    };

    function RetromStateSprite() {
        this.initialize.apply(this, arguments);
    }
    RetromStateSprite.prototype = Object.create(Sprite.prototype);
    RetromStateSprite.prototype.constructor = RetromStateSprite;
    RetromStateSprite.prototype.initialize = function() {
        Sprite.prototype.initialize.call(this, new Bitmap(160, 72));
        this.x = 24;
        this.y = 120;
        this._lastState = -1;
    };
    RetromStateSprite.prototype.update = function() {
        Sprite.prototype.update.call(this);
        var state = Number($gameVariables.value(1)) || 0;
        if (state === this._lastState) return;
        this._lastState = state;
        var bitmap = this.bitmap;
        bitmap.clear();
        bitmap.fillRect(0, 0, 160, 72, "#111827");
        bitmap.fillRect(0, 0, 160, 4, "#40d0ff");
        var segments = [
            [1, 1, 4, 1], [5, 2, 1, 4], [5, 7, 1, 4], [1, 11, 4, 1],
            [0, 7, 1, 4], [0, 2, 1, 4], [1, 6, 4, 1]
        ];
        var enabled = state === 1 ? [1, 2] : state === 2 ? [0, 1, 6, 4, 3] : [0, 1, 2, 3, 4, 5];
        enabled.forEach(function(index) {
            var segment = segments[index];
            bitmap.fillRect(54 + segment[0] * 6, 8 + segment[1] * 4, segment[2] * 6, segment[3] * 4, "#ffffff");
        });
    };

    Scene_Map.prototype.createDisplayObjects = function() {
        this._spriteset = new Sprite();
        this.addChild(this._spriteset);
        this._spriteset.addChild(makeBackground());
        var marker = new Sprite(ImageManager.loadPicture(markerName));
        marker.x = 24;
        marker.y = 24;
        this._spriteset.addChild(marker);
        this._spriteset.addChild(new RetromStateSprite());
        this._spriteset.addChild(new RetromPlayerSprite());
    };
    Scene_Map.prototype.start = function() {
        Scene_Base.prototype.start.call(this);
        SceneManager.clearStack();
        this.menuCalling = false;
    };
    Scene_Map.prototype.stop = function() {
        Scene_Base.prototype.stop.call(this);
        $gamePlayer.straighten();
    };
    Scene_Map.prototype.terminate = function() {
        Scene_Base.prototype.terminate.call(this);
        if (this._spriteset) this.removeChild(this._spriteset);
    };

    var originalUpdate = Scene_Map.prototype.update;
    Scene_Map.prototype.update = function() {
        originalUpdate.call(this);
        if (Input.isTriggered("ok")) {
            var current = Number($gameVariables.value(1)) || 0;
            $gameVariables.setValue(1, current === 0 ? 1 : current === 1 ? 2 : 1);
            var tone = new WebAudio(toneUrl);
            tone.addLoadListener(function() { tone.play(false, 0); });
        }
    };
})();
