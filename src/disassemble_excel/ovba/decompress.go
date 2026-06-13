// Package ovba implements the minimal subset of [MS-OVBA] needed to extract
// VBA macro source code from a vbaProject.bin (OLE2/CFB) stream — without any
// external tool such as olevba. It provides:
//
//   - Decompress: the CompressedContainer decompression algorithm ([MS-OVBA] 2.4.1)
//   - ParseDir:   parsing of the (decompressed) "dir" stream to locate each module
//
// Everything here is pure stdlib so it can be unit-tested against the spec's
// reference vectors.
package ovba

import "fmt"

// Decompress expands a CompressedContainer as described in [MS-OVBA] 2.4.1.3.
// The input must start with the 0x01 signature byte.
func Decompress(buf []byte) ([]byte, error) {
	if len(buf) == 0 || buf[0] != 0x01 {
		return nil, fmt.Errorf("ovba: invalid CompressedContainer signature")
	}
	out := make([]byte, 0, len(buf)*4)
	cur := 1
	for cur+1 < len(buf) {
		chunkStart := cur
		header := uint16(buf[cur]) | uint16(buf[cur+1])<<8
		// bits 0..11: (size of the whole chunk including the 2-byte header) - 3
		chunkSize := int(header&0x0FFF) + 3
		compressed := header&0x8000 != 0
		end := chunkStart + chunkSize
		if end > len(buf) {
			end = len(buf)
		}
		cur = chunkStart + 2

		if !compressed {
			// Raw chunk: the data (up to 4096 bytes) is copied verbatim.
			out = append(out, buf[cur:end]...)
			cur = end
			continue
		}

		chunkDecompStart := len(out)
		for cur < end {
			flag := buf[cur]
			cur++
			for bit := uint(0); bit < 8 && cur < end; bit++ {
				if flag&(1<<bit) == 0 {
					// LiteralToken: a single byte copied as-is.
					out = append(out, buf[cur])
					cur++
					continue
				}
				// CopyToken: 2 bytes encoding (length, offset).
				if cur+1 >= end {
					return out, fmt.Errorf("ovba: truncated CopyToken")
				}
				token := uint16(buf[cur]) | uint16(buf[cur+1])<<8
				cur += 2
				lenMask, offMask, bitCount := copyTokenHelp(len(out) - chunkDecompStart)
				length := int(token&lenMask) + 3
				offset := int((token&offMask)>>(16-bitCount)) + 1
				src := len(out) - offset
				if src < 0 {
					return out, fmt.Errorf("ovba: invalid CopyToken offset")
				}
				// Overlapping copy is intentional (LZ77-style): copy byte by byte.
				for k := 0; k < length; k++ {
					out = append(out, out[src+k])
				}
			}
		}
	}
	return out, nil
}

// copyTokenHelp computes the bit split for a CopyToken given the number of
// bytes already decompressed in the current chunk ([MS-OVBA] 2.4.1.3.19.3).
func copyTokenHelp(difference int) (lengthMask, offsetMask uint16, bitCount uint) {
	bitCount = 4
	for (1 << bitCount) < difference {
		bitCount++
	}
	lengthMask = uint16(0xFFFF >> bitCount)
	offsetMask = ^lengthMask
	return lengthMask, offsetMask, bitCount
}
