package dependencies

func mameAppleBIOSCatalog() []staticBIOS {
	const source = "https://github.com/libretro/mame/blob/f65d5ba9bc42febea7cd76d4559827d0e1271581/"
	return []staticBIOS{
		{
			coreID: "mame_apple2", logical: "341-0011.d0", mode: "REQUIRED", size: 2048,
			md5: "89ca5cd551ffad9a557652a97dcb6627", sha256: "b45168834f01e11ae2cc35fc6bef153e5a13c180503c6533dff111558099df4d",
			sourceURL: source + "src/mame/apple/apple2.cpp",
			delivery:  "EXTERNAL_FILE", emulatorPath: "/content/apple2p/341-0011.d0",
		},
		{
			coreID: "mame_apple2", logical: "341-0012.d8", mode: "REQUIRED", size: 2048,
			md5: "42333f24cd6e70696b212b042f3166aa", sha256: "468d36201974ecbe22efd9164f0ead1abab00b33f1a480da525502964641f444",
			sourceURL: source + "src/mame/apple/apple2.cpp",
			delivery:  "EXTERNAL_FILE", emulatorPath: "/content/apple2p/341-0012.d8",
		},
		{
			coreID: "mame_apple2", logical: "341-0013.e0", mode: "REQUIRED", size: 2048,
			md5: "5de50bebc41e59ae4eb27be4c24b6814", sha256: "2814de134e79213eddb6d7d7a18cba105e120a08e77c9767c46d6fc3cfcc593d",
			sourceURL: source + "src/mame/apple/apple2.cpp",
			delivery:  "EXTERNAL_FILE", emulatorPath: "/content/apple2p/341-0013.e0",
		},
		{
			coreID: "mame_apple2", logical: "341-0014.e8", mode: "REQUIRED", size: 2048,
			md5: "56d9bb6730735a3b2bbcc75d1da7a8de", sha256: "6848707531d7a8934a58e743483e4ebc74bf2ded0229b42533fa20cb89ed1a23",
			sourceURL: source + "src/mame/apple/apple2.cpp",
			delivery:  "EXTERNAL_FILE", emulatorPath: "/content/apple2p/341-0014.e8",
		},
		{
			coreID: "mame_apple2", logical: "341-0015.f0", mode: "REQUIRED", size: 2048,
			md5: "cb63c41c5e72b5fda54feb5490efdefb", sha256: "220fb70bac6839c98901cd542c3c1fbd7145d0bb9423ea8fcc8af0f16ec47d75",
			sourceURL: source + "src/mame/apple/apple2.cpp",
			delivery:  "EXTERNAL_FILE", emulatorPath: "/content/apple2p/341-0015.f0",
		},
		{
			coreID: "mame_apple2", logical: "341-0020-00.f8", mode: "REQUIRED", size: 2048,
			md5: "8925b695ae0177dd3919dbea2f2f202b", sha256: "29465303e7844fa56a8c846d0565e45f5ee082f98f2ccf1b261de4a7e902201b",
			sourceURL: source + "src/mame/apple/apple2.cpp",
			delivery:  "EXTERNAL_FILE", emulatorPath: "/content/apple2p/341-0020-00.f8",
		},
		{
			coreID: "mame_apple2", logical: "341-0036.chr", mode: "REQUIRED", size: 2048,
			md5: "9ac0dc8c4d0002eb45b0b84be0bde5ec", sha256: "08f5d22230481019844492dde0a29a018cb193712a9e4a43770a3870608f28de",
			sourceURL: source + "src/mame/apple/apple2.cpp",
			delivery:  "EXTERNAL_FILE", emulatorPath: "/content/apple2p/341-0036.chr",
		},
		{
			coreID: "mame_apple2", logical: "341-0027-a.p5", mode: "REQUIRED", size: 256,
			md5: "2020aa1413ff77fe29353f3ee72dc295", sha256: "de1e3e035878bab43d0af8fe38f5839c527e9548647036598ee6fe7ec74d2a7d",
			sourceURL: source + "src/devices/bus/a2bus/a2diskiing.cpp",
			delivery:  "EXTERNAL_FILE", emulatorPath: "/content/a2diskiing/341-0027-a.p5",
		},
		{
			coreID: "mame_apple2", logical: "341-0028-a.rom", mode: "REQUIRED", size: 256,
			md5: "5f1be0c1cdff26f5956eef9643911886", sha256: "e5e30615040567c1e7a2d21599681f8dac820edbdcda177b816a64d74b3a12f2",
			sourceURL: source + "src/devices/machine/wozfdc.cpp",
			delivery:  "EXTERNAL_FILE", emulatorPath: "/content/d2fdc/341-0028-a.rom",
		},
	}
}
