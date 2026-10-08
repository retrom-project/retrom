package main

import (
	"bytes"
	"image"
	"image/png"
	"testing"
)

func TestLCFChipsetUsesNativeTransparentIndex(t *testing.T) {
	data, err := chipsetPNG("RETROM RPG2000", [3]byte{40, 176, 136})
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	chipset, ok := decoded.(*image.Paletted)
	if !ok {
		t.Fatal("LCF chipset must retain palette indices")
	}
	// RPG Maker treats palette index zero as transparent, regardless of PNG tRNS.
	// Upper tile 10000 is drawn above the player; an opaque tile hides the map.
	for y := 128; y < 144; y++ {
		for x := 288; x < 304; x++ {
			if index := chipset.ColorIndexAt(x, y); index != 0 {
				t.Fatalf("upper blank tile is opaque in the engine: palette index %d", index)
			}
		}
	}
	for y := 0; y < 16; y++ {
		for x := 192; x < 208; x++ {
			if chipset.ColorIndexAt(x, y) == 0 {
				t.Fatal("lower floor tile must remain opaque in the engine")
			}
		}
	}
}
