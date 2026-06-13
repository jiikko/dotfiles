package ovba

import (
	"encoding/binary"
	"testing"
)

// rec builds a normal "dir" record: Id(2) + Size(4) + Data(Size), little-endian.
func rec(id uint16, data []byte) []byte {
	b := make([]byte, 6+len(data))
	binary.LittleEndian.PutUint16(b[0:], id)
	binary.LittleEndian.PutUint32(b[2:], uint32(len(data)))
	copy(b[6:], data)
	return b
}

func le16(v uint16) []byte {
	b := make([]byte, 2)
	binary.LittleEndian.PutUint16(b, v)
	return b
}

func le32(v uint32) []byte {
	b := make([]byte, 4)
	binary.LittleEndian.PutUint32(b, v)
	return b
}

// projectVersionRecord builds the irregular PROJECTVERSION (0x0009) record:
// Id + Reserved(4) + VersionMajor(4) + VersionMinor(2), with no Size field.
// ParseDir must skip exactly these 12 bytes to stay aligned.
func projectVersionRecord() []byte {
	b := make([]byte, 12)
	binary.LittleEndian.PutUint16(b[0:], 0x0009)
	binary.LittleEndian.PutUint32(b[2:], 4)
	return b
}

func TestParseDir(t *testing.T) {
	var dir []byte
	dir = append(dir, rec(0x0003, le16(932))...)         // PROJECTCODEPAGE
	dir = append(dir, projectVersionRecord()...)         // irregular -> must be skipped
	dir = append(dir, rec(0x0019, []byte("Module1"))...) // MODULENAME
	dir = append(dir, rec(0x001A, []byte("Module1"))...) // MODULESTREAMNAME
	dir = append(dir, rec(0x0031, le32(1234))...)        // MODULEOFFSET
	dir = append(dir, rec(0x0021, nil)...)               // MODULETYPE (procedural)
	dir = append(dir, rec(0x0019, []byte("Sheet1"))...)  // 2nd module
	dir = append(dir, rec(0x001A, []byte("Sheet1"))...)
	dir = append(dir, rec(0x0031, le32(0))...)
	dir = append(dir, rec(0x0022, nil)...) // MODULETYPE (document/class)

	cp, mods := ParseDir(dir)

	if cp != 932 {
		t.Errorf("codePage got=%d want=932", cp)
	}
	if len(mods) != 2 {
		t.Fatalf("modules got=%d want=2 (PROJECTVERSION skip likely misaligned)", len(mods))
	}
	if got := string(mods[0].Name); got != "Module1" {
		t.Errorf("mods[0].Name got=%q want=Module1", got)
	}
	if got := string(mods[0].StreamName); got != "Module1" {
		t.Errorf("mods[0].StreamName got=%q want=Module1", got)
	}
	if mods[0].TextOffset != 1234 {
		t.Errorf("mods[0].TextOffset got=%d want=1234", mods[0].TextOffset)
	}
	if mods[0].Type != "standard" {
		t.Errorf("mods[0].Type got=%q want=standard", mods[0].Type)
	}
	if mods[1].Type != "class" {
		t.Errorf("mods[1].Type got=%q want=class", mods[1].Type)
	}
}
