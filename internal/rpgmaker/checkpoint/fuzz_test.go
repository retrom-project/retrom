package checkpoint

import "testing"

func FuzzDecodeNeverPanics(f *testing.F) {
	valid, err := Encode(EngineRPGMV, 2, []Entry{{
		Store: StoreLocalStorage, Key: "save", MediaType: "application/octet-stream", Data: []byte("payload"),
	}})
	if err != nil {
		f.Fatal(err)
	}
	f.Add(valid)
	f.Add([]byte(Magic))
	f.Add([]byte{})
	f.Fuzz(func(_ *testing.T, contents []byte) {
		_, _ = Decode(contents)
	})
}
