// Retrom-owned deterministic RPG Maker MV smoke bootstrap.
PluginManager.setup($plugins);

window.onload = function() {
    SceneManager.run(Scene_Boot);
};
