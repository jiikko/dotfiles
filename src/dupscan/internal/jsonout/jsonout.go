// Package jsonout は dupscan が書く JSON の形を 1 か所で決める。
//
// `<` `>` `&` を escape しない (encoding/json の既定は `\u003c` `\u003e` `\u0026` にする)。dupscan の JSON は HTML に埋め込まず、
// 人が読み・grep し・「除外の 1 行」は .dupscan.json にそのまま貼る。escape すると、Swift の署名の `->` や `Array<Int>`・本体の `&&` が
// 貼った設定とずれて grep で当たらなくなる (JSON として読んだ値は同じ)。
package jsonout

import (
	"bytes"
	"encoding/json"
	"io"
)

// Write は v を字下げした JSON で w に書く (末尾に改行を 1 つ付ける)。
func Write(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// Line は v を改行を含まない 1 行の JSON にする (除外の 1 行・gate の記録の 1 行)。
func Line(v any) ([]byte, error) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(b.Bytes(), []byte("\n")), nil
}
