package main

import (
	"fmt"
	"path/filepath"
)

type eventCommand struct {
	code       int32
	text       string
	parameters []int32
}

func generateLCF(output string, spec lcfSpec) error {
	if (spec.Generation != "RPG2000" && spec.Generation != "RPG2003") ||
		spec.Directory == "" || spec.Marker == "" ||
		len([]rune(spec.Marker)) > 20 ||
		(spec.LDBID != 0 && spec.LDBID != 2003) {
		return fmt.Errorf("invalid LCF fixture spec for %q", spec.Generation)
	}
	chipset, err := chipsetPNG(spec.Marker, spec.AccentRGB)
	if err != nil {
		return err
	}
	marker, err := lcfMarkerPNG(spec.Marker, spec.AccentRGB)
	if err != nil {
		return err
	}
	root := filepath.ToSlash(filepath.Join(spec.Directory))
	files := map[string][]byte{
		"RPG_RT.ldb":                 buildDatabase(spec),
		"RPG_RT.lmt":                 buildMapTree(spec),
		"Map0001.lmu":                buildMap(spec),
		"RPG_RT.ini":                 []byte("[RPG_RT]\r\nGameTitle=" + spec.Marker + "\r\nFullPackageFlag=1\r\n"),
		"EasyRPG.ini":                []byte("[Game]\nNewGame=1\n"),
		"ChipSet/retrom-chipset.png": chipset,
		"CharSet/retrom-hero.png":    charsetPNG(spec.AccentRGB),
		"System/retrom-system.png":   systemPNG(spec.AccentRGB),
		"Picture/retrom-marker.png":  marker,
		"Sound/retrom-tone.wav":      toneWAV(),
	}
	for name, contents := range files {
		if err := writeFile(output, root+"/"+name, contents); err != nil {
			return err
		}
	}
	return nil
}

func buildDatabase(spec lcfSpec) []byte {
	parameters := make([]int16, 6*50)
	for index := range parameters {
		parameters[index] = 10
	}
	actor := lcfStruct(
		lcfStringField(0x01, "RETROM"),
		lcfStringField(0x03, "retrom-hero"),
		lcfIntegerField(0x07, 1),
		lcfIntegerField(0x08, 50),
		lcfField(0x1f, littleEndianInt16(parameters)),
	)
	terrain := make([]int16, 162)
	for index := range terrain {
		terrain[index] = 1
	}
	passableLower := make([]byte, 162)
	for index := range passableLower {
		passableLower[index] = 15
	}
	passableUpper := make([]byte, 144)
	for index := range passableUpper {
		passableUpper[index] = 15
	}
	passableUpper[0] = 31
	chipset := lcfStruct(
		lcfStringField(0x01, "RETROM"),
		lcfStringField(0x02, "retrom-chipset"),
		lcfField(0x03, littleEndianInt16(terrain)),
		lcfField(0x04, passableLower),
		lcfField(0x05, passableUpper),
	)
	party := littleEndianInt16([]int16{1})
	terrainEntry := lcfStruct(lcfStringField(0x01, "RETROM"))
	system := lcfStruct(
		lcfIntegerField(0x0a, spec.LDBID),
		lcfStringField(0x13, "retrom-system"),
		lcfIntegerField(0x15, 1),
		lcfField(0x16, party),
		lcfIntegerField(0x6f, 0),
	)
	switchEntry := lcfStruct(lcfStringField(0x01, "RETROM READY"))
	variableEntry := lcfStruct(lcfStringField(0x01, "RETROM STATE"))
	database := lcfStruct(
		lcfField(0x0b, lcfStructArray(actor)),
		lcfField(0x10, lcfStructArray(terrainEntry)),
		lcfField(0x14, lcfStructArray(chipset)),
		lcfField(0x16, system),
		lcfField(0x17, lcfStructArray(switchEntry)),
		lcfField(0x18, lcfStructArray(variableEntry)),
	)
	return append(lcfHeader("LcfDataBase"), database...)
}

func buildMapTree(spec lcfSpec) []byte {
	mapInfo := lcfStruct(
		lcfStringField(0x01, spec.Marker),
		lcfIntegerField(0x02, 0),
		lcfIntegerField(0x04, 1),
		lcfIntegerField(0x0b, 1),
		lcfIntegerField(0x15, 1),
		lcfIntegerField(0x1f, 1),
		lcfIntegerField(0x20, 1),
		lcfIntegerField(0x21, 1),
	)
	start := lcfStruct(
		lcfIntegerField(0x01, 1),
		lcfIntegerField(0x02, 10),
		lcfIntegerField(0x03, 8),
	)
	result := lcfHeader("LcfMapTree")
	result = append(result, lcfStructArray(mapInfo)...)
	result = append(result, lcfInt(1)...)
	result = append(result, lcfInt(1)...)
	result = append(result, lcfInt(1)...)
	return append(result, start...)
}

func buildMap(spec lcfSpec) []byte {
	lower := make([]int16, 20*15)
	upper := make([]int16, 20*15)
	for index := range lower {
		lower[index] = 5000
		upper[index] = 10000
	}
	marker := []rune(spec.Marker)
	markerX, markerY := (20-len(marker))/2, 2
	for index := range marker {
		lower[markerY*20+markerX+index] = int16(5001 + index)
	}
	autorun := eventPage(3, nil, []eventCommand{
		{code: 11550, text: "retrom-tone", parameters: []int32{100, 100, 50}},
		{code: 11110, text: "retrom-marker", parameters: []int32{
			1, 0, 160, 36, 0, 100, 0, 1, 100, 100, 100, 100, 0, 0,
		}},
		{code: 10210, parameters: []int32{0, 1, 1, 0}},
	})
	disabled := eventPage(0, lcfStruct(
		lcfField(0x01, []byte{1}),
		lcfIntegerField(0x02, 1),
	), nil)
	markerEvent := lcfStruct(
		lcfStringField(0x01, spec.Marker),
		lcfIntegerField(0x02, 0),
		lcfIntegerField(0x03, 0),
		lcfField(0x05, lcfStructArray(autorun, disabled)),
	)
	variableAtLeast := func(value int32) []byte {
		return lcfStruct(
			lcfField(0x01, []byte{4}),
			lcfIntegerField(0x04, 1),
			lcfIntegerField(0x05, value),
		)
	}
	stateEvent := func(x, minimum int32) []byte {
		var activeCondition []byte
		if minimum > 0 {
			activeCondition = variableAtLeast(minimum)
		}
		return lcfStruct(
			lcfStringField(0x01, "RETROM STATE CHANGED"),
			lcfIntegerField(0x02, x),
			lcfIntegerField(0x03, 8),
			lcfField(0x05, lcfStructArray(
				eventPage(2, activeCondition, []eventCommand{
					{code: 10220, parameters: []int32{0, 1, 1, 1, 0, 1, 0}},
				}),
				eventPage(0, variableAtLeast(minimum+1), nil),
			)),
		)
	}
	mapData := lcfStruct(
		lcfIntegerField(0x01, 1),
		lcfIntegerField(0x02, 20),
		lcfIntegerField(0x03, 15),
		lcfField(0x47, littleEndianInt16(lower)),
		lcfField(0x48, littleEndianInt16(upper)),
		lcfField(0x51, lcfStructArray(markerEvent, stateEvent(11, 0), stateEvent(13, 1))),
	)
	return append(lcfHeader("LcfMapUnit"), mapData...)
}

func eventPage(trigger int32, condition []byte, commands []eventCommand) []byte {
	return configuredEventPage(trigger, condition, "", 0, false, commands)
}

func configuredEventPage(
	trigger int32,
	condition []byte,
	graphic string,
	layer int32,
	overlapForbidden bool,
	commands []eventCommand,
) []byte {
	fields := make([][]byte, 0, 5)
	if condition != nil {
		fields = append(fields, lcfField(0x02, condition))
	}
	if graphic != "" {
		fields = append(fields, lcfStringField(0x15, graphic))
	}
	fields = append(fields, lcfIntegerField(0x1f, 0))
	if layer != 0 {
		fields = append(fields, lcfIntegerField(0x22, layer))
	}
	if overlapForbidden {
		fields = append(fields, lcfField(0x23, []byte{1}))
	}
	if trigger != 0 {
		fields = append(fields, lcfIntegerField(0x21, trigger))
	}
	if commands != nil {
		wire := make([]byte, 0)
		for _, command := range commands {
			wire = append(wire, encodeEventCommand(command)...)
		}
		wire = append(wire, 0, 0, 0, 0)
		fields = append(fields, lcfIntegerField(0x33, int32(len(commands))))
		fields = append(fields, lcfField(0x34, wire))
	}
	return lcfStruct(fields...)
}

func encodeEventCommand(command eventCommand) []byte {
	result := append([]byte{}, lcfInt(command.code)...)
	result = append(result, lcfInt(0)...)
	result = append(result, lcfInt(int32(len(command.text)))...)
	result = append(result, command.text...)
	result = append(result, lcfInt(int32(len(command.parameters)))...)
	for _, parameter := range command.parameters {
		result = append(result, lcfInt(parameter)...)
	}
	return result
}

func lcfHeader(name string) []byte {
	return append(lcfInt(int32(len(name))), name...)
}
