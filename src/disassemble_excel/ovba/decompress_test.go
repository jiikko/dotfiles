package ovba

import (
	"bytes"
	"testing"
)

// The fixtures below are hand-built CompressedContainers per [MS-OVBA] 2.4.1.3,
// so the tests need no external file or tool.
//
// Chunk header layout (little-endian uint16):
//
//	bits 0..11 : CompressedChunkSize  (= total chunk bytes incl. 2-byte header, minus 3)
//	bits 12..14: signature, always 0b011
//	bit  15    : CompressedChunkFlag  (1 = compressed, 0 = raw)
func TestDecompress(t *testing.T) {
	tests := []struct {
		name string
		in   []byte
		want []byte
	}{
		{
			name: "raw chunk",
			// sig 0x01; header 0x3002 (raw, size bits 2 -> 5 total); data "ABC"
			in:   []byte{0x01, 0x02, 0x30, 'A', 'B', 'C'},
			want: []byte("ABC"),
		},
		{
			name: "compressed chunk, literals only",
			// header 0xB008 (compressed, size bits 8 -> 11 total); FlagByte 0x00; 8 literals
			in:   []byte{0x01, 0x08, 0xB0, 0x00, 'a', 'b', 'c', 'd', 'e', 'f', 'g', 'h'},
			want: []byte("abcdefgh"),
		},
		{
			name: "compressed chunk with copy token",
			// FlagByte 0x08: a,b,c as literals, then CopyToken(offset=3,len=3) -> "abcabc"
			in:   []byte{0x01, 0x05, 0xB0, 0x08, 'a', 'b', 'c', 0x00, 0x20},
			want: []byte("abcabc"),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Decompress(tc.in)
			if err != nil {
				t.Fatalf("Decompress error: %v", err)
			}
			if !bytes.Equal(got, tc.want) {
				t.Errorf("Decompress = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestDecompressRejectsBadInput(t *testing.T) {
	if _, err := Decompress([]byte{0x00, 0x01}); err == nil {
		t.Error("expected error for a bad signature byte")
	}
	if _, err := Decompress(nil); err == nil {
		t.Error("expected error for empty input")
	}
}
