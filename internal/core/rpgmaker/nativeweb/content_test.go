package nativeweb

import "testing"

func TestRuntimeFileAllowsOnlyNativeWebProjectResources(t *testing.T) {
	t.Parallel()
	for _, logicalName := range []string{
		"index.html", "js/main.js", "data/System.json", "audio/bgm/theme.ogg", "img/encrypted.rpgmvp",
	} {
		if !RuntimeFile(logicalName) {
			t.Errorf("RuntimeFile(%q) = false", logicalName)
		}
	}
	for _, logicalName := range []string{
		"package.json", "Game.exe", "nw.dll", "plugin.node", "launcher.bat", "README",
	} {
		if RuntimeFile(logicalName) {
			t.Errorf("RuntimeFile(%q) = true", logicalName)
		}
	}
}

func TestNativeExecutableUsesClosedDesktopSuffixSet(t *testing.T) {
	t.Parallel()
	for _, logicalName := range []string{
		"Game.EXE", "nw.dll", "plugin.node", "launcher.bat", "helper.cmd", "setup.ps1",
		"libgame.so", "libgame.dylib",
	} {
		if !NativeExecutable(logicalName) {
			t.Errorf("NativeExecutable(%q) = false", logicalName)
		}
	}
	for _, logicalName := range []string{"js/main.js", "data/System.json", "README"} {
		if NativeExecutable(logicalName) {
			t.Errorf("NativeExecutable(%q) = true", logicalName)
		}
	}
}
