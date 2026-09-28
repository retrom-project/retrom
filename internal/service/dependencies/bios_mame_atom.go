package dependencies

func mameAtomBIOSCatalog() []staticBIOS {
	const source = "https://github.com/libretro/mame/blob/f65d5ba9bc42febea7cd76d4559827d0e1271581/src/mame/acorn/atom.cpp"
	return []staticBIOS{
		{
			coreID: "mame_atom", logical: "abasic.ic20", mode: "REQUIRED", size: 8192,
			md5: "b7b7f8a608339fa39d44a3bcfa2cc3f0", sha256: "2ad04edcde7c8ec7d1fe00422a9975d6bf08d613d336476f8435570053363fe6",
			sourceURL: source, delivery: "EXTERNAL_FILE", emulatorPath: "/content/atom/abasic.ic20",
		},
		{
			coreID: "mame_atom", logical: "afloat.ic21", mode: "REQUIRED", size: 4096,
			md5: "baa26f458acf5745388177ffc7368124", sha256: "391ba8f8734469ed276cc60b8a9932feac13d59dea81b1693058abb0bc5f1a85",
			sourceURL: source, delivery: "EXTERNAL_FILE", emulatorPath: "/content/atom/afloat.ic21",
		},
	}
}

func mameBIOSCatalog() []staticBIOS {
	return append(mameAppleBIOSCatalog(), mameAtomBIOSCatalog()...)
}
