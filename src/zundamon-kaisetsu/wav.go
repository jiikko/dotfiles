package main

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
)

// parseWav は Python の wave モジュールが読める範囲の RIFF/WAVE を読み、(チャンネル数, 標本の幅, 標本化周波数,
// ヘッダの言うフレーム数, 実際に入っている PCM) を返す。
func parseWav(b []byte) (ch, width, rate, nframes int, pcm []byte, err error) {
	if len(b) < 12 || string(b[0:4]) != "RIFF" || string(b[8:12]) != "WAVE" {
		return 0, 0, 0, 0, nil, errors.New("file does not start with RIFF id")
	}
	pos := 12
	gotFmt := false
	for pos+8 <= len(b) {
		id := string(b[pos : pos+4])
		size := int(binary.LittleEndian.Uint32(b[pos+4 : pos+8]))
		body := b[pos+8:]
		switch id {
		case "fmt ":
			if size < 16 || len(body) < 16 {
				return 0, 0, 0, 0, nil, errors.New("fmt chunk too short")
			}
			tag := binary.LittleEndian.Uint16(body[0:2])
			if tag == 0xFFFE {
				// 拡張形式は subformat (16 バイトの GUID) が本当の形式。Python 3.14 の wave と同じく GUID 全体を PCM と比べる
				if size < 40 || len(body) < 40 || !bytes.Equal(body[24:40], ksdataformatSubtypePCM) {
					return 0, 0, 0, 0, nil, errors.New("unknown extended format")
				}
				tag = 1
			}
			if tag != 1 {
				return 0, 0, 0, 0, nil, fmt.Errorf("unknown format: %d", tag)
			}
			ch = int(binary.LittleEndian.Uint16(body[2:4]))
			rate = int(binary.LittleEndian.Uint32(body[4:8]))
			bits := int(binary.LittleEndian.Uint16(body[14:16]))
			width = (bits + 7) / 8
			gotFmt = true
		case "data":
			if !gotFmt {
				return 0, 0, 0, 0, nil, errors.New("data chunk before fmt chunk")
			}
			if ch*width == 0 {
				return 0, 0, 0, 0, nil, errors.New("bad # of channels or sample width")
			}
			nframes = size / (ch * width)
			avail := min(nframes*ch*width, len(body)) // 半端なバイト (奇数長の data) はフレームに数えない
			return ch, width, rate, nframes, body[:avail], nil
		}
		pos += 8 + size + size%2
	}
	return 0, 0, 0, 0, nil, errors.New("fmt chunk and/or data chunk missing") // fmt が無い・data が無いのどちらも (Python の wave と同じ文言)
}

// checkWavBytes は mono / 16bit / sampleRate で、ヘッダの長さどおりに PCM が入っているかを確かめる。
func checkWavBytes(b []byte, what string) ([]byte, int, error) {
	ch, width, rate, n, pcm, err := parseWav(b)
	if err != nil {
		return nil, 0, fail("%s: wav として読めない (%v)", what, err)
	}
	if ch != 1 || width != 2 || rate != sampleRate {
		return nil, 0, fail("%s: mono / 16bit / %dHz ではない (%dch, %dbit, %dHz)", what, sampleRate, ch, width*8, rate)
	}
	if n == 0 || len(pcm) != n*2 {
		got := min(len(pcm), n*2)
		return nil, 0, fail("%s: wav が空か途中で切れている (ヘッダ %d フレーム / 実データ %d フレーム)", what, n, got/2)
	}
	return pcm, n, nil
}

func readWav(path string) ([]byte, float64, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, 0, fail("%s: 読めない (%v)", path, err)
	}
	pcm, n, err := checkWavBytes(b, path+" (synth --force で作り直す)")
	if err != nil {
		return nil, 0, err
	}
	return pcm, float64(n) / sampleRate, nil
}

func writeWav(path string, pcm []byte) error {
	h := make([]byte, 44)
	copy(h[0:4], "RIFF")
	binary.LittleEndian.PutUint32(h[4:8], uint32(36+len(pcm)))
	copy(h[8:16], "WAVEfmt ")
	binary.LittleEndian.PutUint32(h[16:20], 16)
	binary.LittleEndian.PutUint16(h[20:22], 1)
	binary.LittleEndian.PutUint16(h[22:24], 1)
	binary.LittleEndian.PutUint32(h[24:28], sampleRate)
	binary.LittleEndian.PutUint32(h[28:32], sampleRate*2)
	binary.LittleEndian.PutUint16(h[32:34], 2)
	binary.LittleEndian.PutUint16(h[34:36], 16)
	copy(h[36:40], "data")
	binary.LittleEndian.PutUint32(h[40:44], uint32(len(pcm)))
	return os.WriteFile(path, append(h, pcm...), 0o644)
}
