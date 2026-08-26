package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"hash/crc32"
)

func pngRGBA(width, height int, pixel func(x, y int) [4]byte) []byte {
	raw := make([]byte, 0, height*(1+width*4))
	for y := 0; y < height; y++ {
		raw = append(raw, 0)
		for x := 0; x < width; x++ {
			rgba := pixel(x, y)
			raw = append(raw, rgba[:]...)
		}
	}
	var result bytes.Buffer
	result.Write([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'})
	ihdr := make([]byte, 13)
	binary.BigEndian.PutUint32(ihdr[0:4], uint32(width))
	binary.BigEndian.PutUint32(ihdr[4:8], uint32(height))
	ihdr[8] = 8
	ihdr[9] = 6
	writePNGChunk(&result, "IHDR", ihdr)
	writePNGChunk(&result, "IDAT", zlibStore(raw))
	writePNGChunk(&result, "IEND", nil)
	return result.Bytes()
}

func pngIndexed(width, height int, palette [][4]byte, pixel func(x, y int) byte) []byte {
	if len(palette) == 0 || len(palette) > 256 {
		panic("indexed PNG palette must contain 1..256 colors")
	}
	raw := make([]byte, 0, height*(1+width))
	for y := 0; y < height; y++ {
		raw = append(raw, 0)
		for x := 0; x < width; x++ {
			index := pixel(x, y)
			if int(index) >= len(palette) {
				panic("indexed PNG pixel is outside the palette")
			}
			raw = append(raw, index)
		}
	}
	var result bytes.Buffer
	result.Write([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'})
	ihdr := make([]byte, 13)
	binary.BigEndian.PutUint32(ihdr[0:4], uint32(width))
	binary.BigEndian.PutUint32(ihdr[4:8], uint32(height))
	ihdr[8] = 8
	ihdr[9] = 3
	writePNGChunk(&result, "IHDR", ihdr)
	plte := make([]byte, 0, len(palette)*3)
	alpha := make([]byte, 0, len(palette))
	for _, color := range palette {
		plte = append(plte, color[0], color[1], color[2])
		alpha = append(alpha, color[3])
	}
	writePNGChunk(&result, "PLTE", plte)
	writePNGChunk(&result, "tRNS", alpha)
	writePNGChunk(&result, "IDAT", zlibStore(raw))
	writePNGChunk(&result, "IEND", nil)
	return result.Bytes()
}

func writePNGChunk(target *bytes.Buffer, kind string, data []byte) {
	_ = binary.Write(target, binary.BigEndian, uint32(len(data)))
	target.WriteString(kind)
	target.Write(data)
	checksum := crc32.NewIEEE()
	_, _ = checksum.Write([]byte(kind))
	_, _ = checksum.Write(data)
	_ = binary.Write(target, binary.BigEndian, checksum.Sum32())
}

func chipsetPNG(marker string, accent [3]byte) ([]byte, error) {
	glyphs := markerGlyphs()
	for _, character := range marker {
		if _, exists := glyphs[character]; !exists {
			return nil, fmt.Errorf("unsupported LCF marker glyph %q", character)
		}
	}
	markerRunes := []rune(marker)
	palette := [][4]byte{
		{accent[0] / 2, accent[1] / 2, accent[2] / 2, 255},
		{accent[0]/2 + 28, accent[1]/2 + 28, accent[2]/2 + 28, 255},
		{255, 255, 255, 255},
		{0, 0, 0, 0},
	}
	return pngIndexed(480, 256, palette, func(x, y int) byte {
		if x >= 18*16 && x < 19*16 && y >= 8*16 && y < 9*16 {
			return 3
		}
		if tileID, exists := blockETileAtPixel(x, y); exists && tileID > 0 && tileID <= len(markerRunes) {
			localX, localY := x%16, y%16
			if localX >= 3 && localX < 13 && localY >= 1 && localY < 15 {
				glyphX, glyphY := (localX-3)/2, (localY-1)/2
				if glyphs[markerRunes[tileID-1]][glyphY][glyphX] == '1' {
					return 2
				}
			}
		}
		if (x/16+y/16)%2 == 0 {
			return 1
		}
		return 0
	}), nil
}

func blockETileAtPixel(x, y int) (int, bool) {
	column, row := x/16, y/16
	if column >= 12 && column < 18 {
		return row*6 + column - 12, true
	}
	if column >= 18 && column < 24 {
		return 96 + row*6 + column - 18, true
	}
	return 0, false
}

func charsetPNG(accent [3]byte) []byte {
	palette := [][4]byte{
		{0, 0, 0, 0},
		{255, 235, 205, 255},
		{accent[0], accent[1], accent[2], 255},
	}
	return pngIndexed(288, 256, palette, func(x, y int) byte {
		cellX, cellY := x%24, y%32
		if cellX < 5 || cellX > 18 || cellY < 4 || cellY > 28 {
			return 0
		}
		if cellY < 11 {
			return 1
		}
		return 2
	})
}

func systemPNG(accent [3]byte) []byte {
	palette := [][4]byte{
		{accent[0], accent[1], accent[2], 255},
		{255, 255, 255, 255},
	}
	return pngIndexed(160, 80, palette, func(x, y int) byte {
		border := x%32 < 2 || y%32 < 2
		if border {
			return 1
		}
		return 0
	})
}

func lcfMarkerPNG(marker string, accent [3]byte) ([]byte, error) {
	glyphs := markerGlyphs()
	markerRunes := []rune(marker)
	for _, character := range markerRunes {
		if _, exists := glyphs[character]; !exists {
			return nil, fmt.Errorf("unsupported LCF marker glyph %q", character)
		}
	}
	const width = 288
	const height = 48
	const scale = 3
	textWidth := (len(markerRunes)*6 - 1) * scale
	startX := (width - textWidth) / 2
	palette := [][4]byte{
		{0, 0, 0, 0},
		{accent[0] / 2, accent[1] / 2, accent[2] / 2, 255},
		{255, 255, 255, 255},
	}
	return pngIndexed(width, height, palette, func(x, y int) byte {
		if x < 4 || x >= width-4 || y < 4 || y >= height-4 {
			return 0
		}
		if x >= startX && x < startX+textWidth && y >= 13 && y < 13+7*scale {
			characterIndex := (x - startX) / (6 * scale)
			glyphX := ((x - startX) % (6 * scale)) / scale
			glyphY := (y - 13) / scale
			if characterIndex < len(markerRunes) && glyphX < 5 &&
				glyphs[markerRunes[characterIndex]][glyphY][glyphX] == '1' {
				return 2
			}
		}
		return 1
	}), nil
}

func toneWAV() []byte {
	const sampleRate = 22050
	const sampleCount = sampleRate / 4
	data := make([]byte, sampleCount*2)
	phase := 0
	for index := 0; index < sampleCount; index++ {
		phase = (phase + 440) % sampleRate
		value := int16(6000)
		if phase >= sampleRate/2 {
			value = -value
		}
		binary.LittleEndian.PutUint16(data[index*2:], uint16(value))
	}
	var result bytes.Buffer
	result.WriteString("RIFF")
	_ = binary.Write(&result, binary.LittleEndian, uint32(36+len(data)))
	result.WriteString("WAVEfmt ")
	_ = binary.Write(&result, binary.LittleEndian, uint32(16))
	_ = binary.Write(&result, binary.LittleEndian, uint16(1))
	_ = binary.Write(&result, binary.LittleEndian, uint16(1))
	_ = binary.Write(&result, binary.LittleEndian, uint32(sampleRate))
	_ = binary.Write(&result, binary.LittleEndian, uint32(sampleRate*2))
	_ = binary.Write(&result, binary.LittleEndian, uint16(2))
	_ = binary.Write(&result, binary.LittleEndian, uint16(16))
	result.WriteString("data")
	_ = binary.Write(&result, binary.LittleEndian, uint32(len(data)))
	result.Write(data)
	return result.Bytes()
}

func markerPNG(marker string, accent [3]byte) ([]byte, error) {
	glyphs := markerGlyphs()
	const scale = 6
	const margin = 8
	width := margin*2 + len([]rune(marker))*6*scale
	height := margin*2 + 7*scale
	for _, character := range marker {
		if _, exists := glyphs[character]; !exists {
			return nil, fmt.Errorf("unsupported marker glyph %q", character)
		}
	}
	return pngRGBA(width, height, func(x, y int) [4]byte {
		if x < margin || y < margin || y >= margin+7*scale {
			return [4]byte{}
		}
		characterIndex := (x - margin) / (6 * scale)
		if characterIndex >= len([]rune(marker)) {
			return [4]byte{}
		}
		glyphX := ((x - margin) % (6 * scale)) / scale
		glyphY := (y - margin) / scale
		if glyphX >= 5 || glyphs[[]rune(marker)[characterIndex]][glyphY][glyphX] != '1' {
			return [4]byte{}
		}
		return [4]byte{accent[0], accent[1], accent[2], 255}
	}), nil
}

func markerGlyphs() map[rune][7]string {
	return map[rune][7]string{
		'0': {"01110", "10001", "10011", "10101", "11001", "10001", "01110"},
		'2': {"01110", "10001", "00001", "00010", "00100", "01000", "11111"},
		'3': {"11110", "00001", "00001", "01110", "00001", "00001", "11110"},
		' ': {"00000", "00000", "00000", "00000", "00000", "00000", "00000"},
		'E': {"11111", "10000", "10000", "11110", "10000", "10000", "11111"},
		'G': {"01110", "10001", "10000", "10111", "10001", "10001", "01110"},
		'M': {"10001", "11011", "10101", "10101", "10001", "10001", "10001"},
		'O': {"01110", "10001", "10001", "10001", "10001", "10001", "01110"},
		'P': {"11110", "10001", "10001", "11110", "10000", "10000", "10000"},
		'R': {"11110", "10001", "10001", "11110", "10100", "10010", "10001"},
		'T': {"11111", "00100", "00100", "00100", "00100", "00100", "00100"},
		'V': {"10001", "10001", "10001", "10001", "10001", "01010", "00100"},
	}
}
