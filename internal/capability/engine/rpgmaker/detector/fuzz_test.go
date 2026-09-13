package detector

import "testing"

func FuzzDetectBoundedFormats(f *testing.F) {
	for _, seed := range [][]byte{
		makeLDBWithIDs(0), makeLDBWithIDs(2003), makeLMT(),
		[]byte("[Game]\nScripts=Data/Scripts.rxdata\n"),
		[]byte(`{"gameTitle":"fixture"}`), []byte(`<script src="js/main.js"></script>`),
		{0xff},
		{0x80, 0x80, 0x80, 0x80, 0x80, 0},
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, contents []byte) {
		projects := []memoryIndex{
			replaceLDB(rpg2KProject(0), contents),
			replaceLMT(rpg2KProject(0), contents),
			with(rgssProject("Data/Scripts.rxdata"), "Game.ini", contents),
			with(mvProject(), "data/System.json", contents),
			with(mvProject(), "index.html", contents),
		}
		cores := []string{"rpgmaker_2000", "rpgmaker_2000", "rpgmaker_xp", "rpgmaker_mv", "rpgmaker_mv"}
		for index, project := range projects {
			profile, err := Detect(cores[index], project)
			if err == nil && profile.ExpectedGeneration == "" {
				t.Fatalf("successful detection returned an empty profile: %#v", profile)
			}
		}
	})
}
