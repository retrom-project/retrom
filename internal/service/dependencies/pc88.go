package dependencies

func pc88BIOSCatalog() []staticBIOS {
	return []staticBIOS{
		{
			coreID: "quasi88", logical: "N88.ROM", mode: "REQUIRED", size: 32768,
			md5: "4f984e04a99d56c4cfe36115415d6eb8", sha256: "cf0b48f5541f5efd54a006d1a6042dd7ce613ccc69e13f2f41ca678569b5d650",
			sourceURL: "https://docs.libretro.com/library/quasi88/",
			delivery:  "EXTERNAL_FILE", emulatorPath: "/retroarch/userdata/system/quasi88/N88.ROM",
		},
		{
			coreID: "quasi88", logical: "N88EXT0.ROM", mode: "REQUIRED", size: 8192,
			md5: "d675a2ca186c6efcd6277b835de4c7e5", sha256: "290286a513c39af02581481958b8f1291c0ee8409b3940ae808352e0c3a2e2f8",
			sourceURL: "https://docs.libretro.com/library/quasi88/",
			delivery:  "EXTERNAL_FILE", emulatorPath: "/retroarch/userdata/system/quasi88/N88EXT0.ROM",
		},
		{
			coreID: "quasi88", logical: "N88EXT1.ROM", mode: "REQUIRED", size: 8192,
			md5: "e844534dfe5744b381444dbe61ef1b66", sha256: "bbb6283a0810eb4468909a364ccbc6d9cec51024aa0a2afa2e72a7af6d23049b",
			sourceURL: "https://docs.libretro.com/library/quasi88/",
			delivery:  "EXTERNAL_FILE", emulatorPath: "/retroarch/userdata/system/quasi88/N88EXT1.ROM",
		},
		{
			coreID: "quasi88", logical: "N88EXT2.ROM", mode: "REQUIRED", size: 8192,
			md5: "6548fa45061274dee1ea8ae1e9e93910", sha256: "ad0f26064f44718eedd0103fde3d41bd29e60317680611e784dc9c1943e7574b",
			sourceURL: "https://docs.libretro.com/library/quasi88/",
			delivery:  "EXTERNAL_FILE", emulatorPath: "/retroarch/userdata/system/quasi88/N88EXT2.ROM",
		},
		{
			coreID: "quasi88", logical: "N88EXT3.ROM", mode: "REQUIRED", size: 8192,
			md5: "fc4b76a402ba501e6ba6de4b3e8b4273", sha256: "8580fecc6574b40a082aaf227afc3c1dbd06f333ba7549278ff73647b02b2303",
			sourceURL: "https://docs.libretro.com/library/quasi88/",
			delivery:  "EXTERNAL_FILE", emulatorPath: "/retroarch/userdata/system/quasi88/N88EXT3.ROM",
		},
		{
			coreID: "quasi88", logical: "N88N.ROM", mode: "REQUIRED", size: 32768,
			md5: "2ff07b8769367321128e03924af668a0", sha256: "11c3c727d7d12d0c7e044dd02ce154a5715bd7a2b8a9007132645962c7803881",
			sourceURL: "https://docs.libretro.com/library/quasi88/",
			delivery:  "EXTERNAL_FILE", emulatorPath: "/retroarch/userdata/system/quasi88/N88N.ROM",
		},
		{
			coreID: "quasi88", logical: "N88SUB.ROM", mode: "REQUIRED", size: 2048,
			md5: "793f86784e5608352a5d7f03f03e0858", sha256: "9dc63118728a171ce2ed437ad38ef280b8b7bcf4a5a95fedf18946501b010456",
			sourceURL: "https://docs.libretro.com/library/quasi88/",
			delivery:  "EXTERNAL_FILE", emulatorPath: "/retroarch/userdata/system/quasi88/N88SUB.ROM",
		},
	}
}
