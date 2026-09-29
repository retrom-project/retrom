package dependencies

const mameUpstreamSource = "https://github.com/libretro/mame/blob/" +
	"f65d5ba9bc42febea7cd76d4559827d0e1271581/src/mame/"

func mameApple2eBIOSCatalog() []staticBIOS {
	const source = mameUpstreamSource + "apple/apple2e.cpp"
	files := [...]staticBIOS{
		{
			coreID: "mame_apple2e", logical: "342-0133-a.chr", mode: "REQUIRED", size: 4096,
			md5: "e6d453d8738e6df4f73df8c8051df3e8", sha256: "0d54ff735c060c55d54b8a22d0112af78a6465ce9c9aae4a865d207e5c8ff1e7",
			sourceURL: source, delivery: "EXTERNAL_FILE", emulatorPath: "/content/apple2e/342-0133-a.chr",
		},
		{
			coreID: "mame_apple2e", logical: "342-0135-b.64", mode: "REQUIRED", size: 8192,
			md5: "0b150f4bfa090770a866cc5d214703f4", sha256: "f119063639c04770ea4a8e6515304560c348ca9bd10da4055623efbbdc198c65",
			sourceURL: source, delivery: "EXTERNAL_FILE", emulatorPath: "/content/apple2e/342-0135-b.64",
		},
		{
			coreID: "mame_apple2e", logical: "342-0134-a.64", mode: "REQUIRED", size: 8192,
			md5: "72924019cf1719765e4fde35e59c1c7d", sha256: "ca06c6e6921d5709538fb1a60d3ecbb4623f13c69fca828985963753c95b6285",
			sourceURL: source, delivery: "EXTERNAL_FILE", emulatorPath: "/content/apple2e/342-0134-a.64",
		},
		{
			coreID: "mame_apple2e", logical: "342-0132-c.e12", mode: "REQUIRED", size: 2048,
			md5: "4431aea380185e3f509285540d7cb418", sha256: "fbb9620e01f4f728e5a8ba86544900978d7803f6a7d577d384e288dfed9a4907",
			sourceURL: source, delivery: "EXTERNAL_FILE", emulatorPath: "/content/apple2e/342-0132-c.e12",
		},
	}
	controllers := mameAppleBIOSCatalog()[7:]
	for i := range controllers {
		controllers[i].coreID = "mame_apple2e"
	}
	result := make([]staticBIOS, 0, len(files)+len(controllers))
	result = append(result, files[:]...)
	return append(result, controllers...)
}

func mameColecoBIOSCatalog() []staticBIOS {
	return []staticBIOS{{
		coreID: "mame_coleco", logical: "313_10031-4005_73108a.u2", mode: "REQUIRED", size: 8192,
		md5: "2c66f5911e5b42b8ebe113403548eee7", sha256: "990bf1956f10207d8781b619eb74f89b00d921c8d45c95c334c16c8cceca09ad",
		sourceURL: mameUpstreamSource + "coleco/coleco.cpp",
		delivery:  "EXTERNAL_FILE", emulatorPath: "/content/coleco/313_10031-4005_73108a.u2",
	}}
}

func mameModel2DeviceBIOSCatalog() []staticBIOS {
	return []staticBIOS{{
		coreID: "mame_arcade", logical: "epr-18022.ic2", mode: "OPTIONAL", size: 65536,
		md5: "b9574b29185fa9e16977d041dd21dabd", sha256: "111427213e2635b18753cbfb7cc4a0bffdd7c71cedec5cacadb11eec0211afda",
		sourceURL: mameUpstreamSource + "sega/segabill.cpp",
		delivery:  "EXTERNAL_FILE", emulatorPath: "/content/roms/segabill/epr-18022.ic2",
	}}
}
