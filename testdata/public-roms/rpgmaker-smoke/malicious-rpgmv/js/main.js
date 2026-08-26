(function (global) {
  "use strict";
  var generation = global.Utils.RPGMAKER_NAME;
  var state = { mapId: 1, x: 10, y: 8, fixtureState: 0 };
  var saved = {};
  function Scene_Map() {}
  global.Scene_Map = Scene_Map;
  global.$gameMap = { mapId: function () { return state.mapId; }, isEventRunning: function () { return false; } };
  global.$gamePlayer = {};
  Object.defineProperties(global.$gamePlayer, {
    x: { get: function () { return state.x; }, set: function (value) { state.x = value; } },
    y: { get: function () { return state.y; }, set: function (value) { state.y = value; } }
  });
  global.$gameVariables = {
    value: function (id) { return id === 1 ? state.fixtureState : 0; },
    setValue: function (id, value) { if (id === 1) state.fixtureState = Number(value) || 0; }
  };
  global.$gameMessage = { isBusy: function () { return false; } };
  global.SceneManager = {
    _scene: new Scene_Map(), _stopped: false,
    updateMain: function () { draw(); }, stop: function () { this._stopped = true; },
    resume: function () { this._stopped = false; }, requestUpdate: function () {},
    goto: function (Scene) { this._scene = new Scene(); }
  };
  function snapshot() { return { mapId: state.mapId, x: state.x, y: state.y, fixtureState: state.fixtureState }; }
  function restore(value) { state.mapId = value.mapId; state.x = value.x; state.y = value.y; state.fixtureState = value.fixtureState; }
  global.StorageManager = generation === "MZ" ? {
    saveObject: async function (name, value) { saved[name] = JSON.stringify(value); },
    loadObject: async function (name) { return JSON.parse(saved[name]); },
    exists: function (name) { return Object.prototype.hasOwnProperty.call(saved, name); }
  } : {
    save: function (id, value) { saved[String(id)] = String(value); },
    load: function (id) { return saved[String(id)]; },
    exists: function (id) { return Object.prototype.hasOwnProperty.call(saved, String(id)); }
  };
  global.DataManager = {
    _globalInfo: null, maxSavefiles: function () { return 20; }, isDatabaseLoaded: function () { return true; },
    saveGame: async function (slot) {
      if (generation === "MZ") {
        await global.StorageManager.saveObject("global", { fixture: true });
        await global.StorageManager.saveObject("file" + slot, snapshot());
      } else {
        global.StorageManager.save(0, JSON.stringify({ fixture: true }));
        global.StorageManager.save(slot, JSON.stringify(snapshot()));
      }
      return true;
    },
    loadGame: async function (slot) {
      var value = generation === "MZ" ? await global.StorageManager.loadObject("file" + slot) :
        JSON.parse(global.StorageManager.load(slot));
      restore(value); return true;
    }
  };
  global.WebAudio = { setMasterVolume: function () {} };
  var canvas = document.querySelector("canvas");
  var context = canvas.getContext("2d");
  function draw() {
    context.fillStyle = "#101827"; context.fillRect(0, 0, canvas.width, canvas.height);
    context.fillStyle = "#40d0ff"; context.fillRect(24, 24, 592, 5);
    context.fillStyle = "#ffffff"; context.font = "24px sans-serif";
    context.fillText("RETROM " + generation + " ISOLATION HARNESS", 24, 70);
    context.fillText("position " + state.x + "," + state.y + " state " + state.fixtureState, 24, 110);
    context.fillStyle = "#40d0ff"; context.fillRect(24 + state.x * 20, 150 + state.y * 12, 18, 18);
  }
  function sound() {
    try {
      var audio = new AudioContext(); var source = audio.createBufferSource();
      source.buffer = audio.createBuffer(1, 128, audio.sampleRate); source.connect(audio.destination); source.start();
    } catch (_) {}
  }
  global.addEventListener("keydown", function (event) {
    if (event.key === "ArrowRight") state.x += 1;
    if (event.key === "ArrowLeft") state.x -= 1;
    if (event.key === "ArrowDown") state.y += 1;
    if (event.key === "ArrowUp") state.y -= 1;
    if (event.key === "Enter" || event.key === " ") state.fixtureState = (state.fixtureState + 1) % 3;
    sound(); draw();
  });
  function tick() { if (!global.SceneManager._stopped) global.SceneManager.updateMain(); global.requestAnimationFrame(tick); }
  draw(); global.requestAnimationFrame(tick);
})(window);
(function (global) {
  "use strict";
  var results = Object.create(null);
  global.__RETROM_MALICIOUS_RESULTS__ = results;
  function record(name, outcome) { results[name] = String(outcome); }
  try { record("parentDom", global.parent.document.body ? "exposed" : "empty"); } catch (_) { record("parentDom", "blocked"); }
  try { record("appCookie", document.cookie || "none"); } catch (_) { record("appCookie", "blocked"); }
  try { global.top.location["ass" + "ign"]("/retrom-isolation-escape"); record("topNavigation", "exposed"); }
  catch (_) { record("topNavigation", "blocked"); }
  try { var popup = global["op" + "en"]("about:blank", "retrom-isolation-probe"); record("popup", popup ? "exposed" : "blocked"); if (popup) popup.close(); }
  catch (_) { record("popup", "blocked"); }
  try {
    var form = document.createElement("form"); form.method = "POST";
    form.action = ["https:", "//example.invalid/retrom-form"].join(""); form.target = "retrom-isolation-form";
    document.body.appendChild(form); form.submit(); record("form", "attempted");
  } catch (_) { record("form", "blocked"); }
  Promise.resolve().then(async function () {
    try { await global["fet" + "ch"](["https:", "//example.invalid/retrom-fetch"].join("")); record("externalFetch", "exposed"); }
    catch (_) { record("externalFetch", "blocked"); }
    try { var response = await global["fet" + "ch"]("/api/v1/health", { credentials: "include" }); record("nonAllowlistApi", response.status); }
    catch (_) { record("nonAllowlistApi", "blocked"); }
    try {
      var registration = await navigator.serviceWorker["reg" + "ister"]("/retrom-malicious-sw.js");
      record("serviceWorker", "exposed"); await registration.unregister();
    } catch (_) { record("serviceWorker", "blocked"); }
    results.complete = "true";
  });
})(window);
