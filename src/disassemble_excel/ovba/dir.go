package ovba

import "encoding/binary"

// DirModule is one MODULE entry parsed out of the decompressed "dir" stream.
// Name and StreamName are raw MBCS bytes (decode them with the project code page,
// which ParseDir returns separately).
type DirModule struct {
	Name       []byte
	StreamName []byte
	Type       string // "standard" (procedural) or "class" (document/class)
	TextOffset uint32 // byte offset in the module stream where compressed source begins
}

// Record IDs from [MS-OVBA] 2.3.4.2 that we care about.
const (
	idProjectCodePage  = 0x0003
	idProjectVersion   = 0x0009 // irregular record (see below)
	idModuleName       = 0x0019
	idModuleStreamName = 0x001A
	idModuleOffset     = 0x0031
	idModuleTypeProc   = 0x0021
	idModuleTypeDoc    = 0x0022
)

// ParseDir walks the decompressed "dir" stream and returns the project code page
// plus one DirModule per VBA module.
//
// Records are uniformly Id(2) + Size(4) + Data(Size), which lets us skip over the
// many records we do not need. The single exception is PROJECTVERSION (0x0009),
// whose 4-byte field is a Reserved value (not a size) followed by 6 bytes of
// version data; we special-case it so the walk stays aligned.
func ParseDir(dir []byte) (codePage int, modules []DirModule) {
	codePage = 1252
	pos := 0
	var cur *DirModule
	for pos+6 <= len(dir) {
		id := binary.LittleEndian.Uint16(dir[pos:])
		size := int(binary.LittleEndian.Uint32(dir[pos+2:]))

		if id == idProjectVersion {
			pos += 2 + 4 + 4 + 2 // Id + Reserved(4) + VersionMajor(4) + VersionMinor(2)
			continue
		}

		dataStart := pos + 6
		if size < 0 || dataStart+size > len(dir) {
			break
		}
		data := dir[dataStart : dataStart+size]

		switch id {
		case idProjectCodePage:
			if len(data) >= 2 {
				codePage = int(binary.LittleEndian.Uint16(data))
			}
		case idModuleName:
			modules = append(modules, DirModule{Name: clone(data), Type: "standard"})
			cur = &modules[len(modules)-1]
		case idModuleStreamName:
			if cur != nil {
				cur.StreamName = clone(data)
			}
		case idModuleOffset:
			if cur != nil && len(data) >= 4 {
				cur.TextOffset = binary.LittleEndian.Uint32(data)
			}
		case idModuleTypeProc:
			if cur != nil {
				cur.Type = "standard"
			}
		case idModuleTypeDoc:
			if cur != nil {
				cur.Type = "class"
			}
		}
		pos = dataStart + size
	}
	return codePage, modules
}

func clone(b []byte) []byte {
	c := make([]byte, len(b))
	copy(c, b)
	return c
}
