package build

import (
	"fmt"
	"strings"

	"zundamon-kaisetsu/internal/app"
	"zundamon-kaisetsu/internal/paths"
	"zundamon-kaisetsu/internal/pyjson"
	"zundamon-kaisetsu/internal/script"
	"zundamon-kaisetsu/internal/show"
	"zundamon-kaisetsu/internal/wav"
)

// 行の時刻: 合成済みの wav の長さと間 (lead_in / gap / pause_after) から、各行の音声の開始・終了の秒を出す。
// Assemble は wav を連結しながら同じ値を作る。間の秒数を標本の数に丸める式 (leadInSamples / pauseSamples) を両方が使い、
// 値が同じことは TestLineTimesMatchAssemble が固定する。synth の字幕の速さの警告と times サブコマンドは、
// 立ち絵・図・音声の連結が要らないので、wav の長さだけを読むこちらを使う。

// LineSpan は行の音声の開始・終了 (秒。Assemble の timeline の start / end と同じ丸め)。
type LineSpan struct{ Start, End float64 }

func leadInSamples(s *script.Script) int {
	return pyjson.Round(numOr(script.LineGet(s.Raw, "lead_in", nil), 0.4) * wav.SampleRate)
}

func pauseSamples(s *script.Script, line map[string]any) int {
	gap := numOr(script.LineGet(s.Raw, "gap", nil), 0.35)
	return pyjson.Round(numOr(script.LineGet(line, "pause_after", nil), gap) * wav.SampleRate)
}

func missingWavErr(missing []int) error {
	return app.Fail("合成済みの wav が無い行がある (台本を変えた後に synth していない?): lines %s", pyIntList(missing[:min(10, len(missing))]))
}

// LineTimes は各行の音声の開始・終了の秒と、動画全体の長さを返す。合成済みの wav が無い行があれば止める。
func LineTimes(s *script.Script) ([]LineSpan, float64, error) {
	wd := paths.WorkDir(s.Path)
	pos := leadInSamples(s)
	spans := make([]LineSpan, 0, len(s.Lines))
	var missing []int
	for i, line := range s.Lines {
		p, err := script.LineParams(s, line)
		if err != nil {
			return nil, 0, err
		}
		wavPath, queryPath := script.CachePaths(wd, p)
		if !paths.IsFile(wavPath) || !paths.IsFile(queryPath) { // Assemble と同じく両方を要る (片方だけでは build できない)
			missing = append(missing, i)
			continue
		}
		pcm, dur, err := wav.Read(wavPath)
		if err != nil {
			return nil, 0, err
		}
		start := float64(pos) / wav.SampleRate
		spans = append(spans, LineSpan{Start: pyjson.Round3(start), End: pyjson.Round3(start + dur)})
		pos += len(pcm)/2 + pauseSamples(s, line)
	}
	if len(missing) > 0 {
		return nil, 0, missingWavErr(missing)
	}
	return spans, float64(pos) / wav.SampleRate, nil
}

// CaptionLines は字幕の速さの検査 (FastCaptions) に渡す行 (字幕と開始の秒だけを埋める)。
func CaptionLines(s *script.Script, spans []LineSpan) []TimelineLine {
	out := make([]TimelineLine, len(spans))
	for i, sp := range spans {
		out[i] = TimelineLine{Text: pyjson.Str(s.Lines[i]["text"]), Start: sp.Start, End: sp.End}
	}
	return out
}

// CmdTimes は行ごとの 行番号 / 開始秒 / 終了秒 / 話者 / チャプター (始まる行だけ) / 図解 (show を書いた行だけ) / 字幕 を TSV で出す。
// mp4 から図解の行を切り出して確かめるとき (ffmpeg -ss 開始秒) や、HTML を #t=開始秒 で開くときに使う。
func CmdTimes(env *app.Env, scriptArg string) error {
	s, err := script.LoadScript(paths.ResolvePath(scriptArg), env)
	if err != nil {
		return err
	}
	spans, _, err := LineTimes(s)
	if err != nil {
		return err
	}
	for i, sp := range spans {
		line := s.Lines[i]
		chapter := ""
		if c, ok := line["chapter"]; ok && pyjson.Truthy(c) {
			chapter = pyjson.Str(c)
		}
		fmt.Fprintf(env.Stdout, "%d\t%.2f\t%.2f\t%s\t%s\t%s\t%s\n", i, sp.Start, sp.End, tsvField(pyjson.Str(line["who"])), tsvField(chapter), showLabel(line), tsvField(pyjson.Str(line["text"])))
	}
	return nil
}

// tsvField は TSV の 1 欄に入れる文字列 (タブと改行を空白にして、欄と行の区切りを崩さない)。
func tsvField(v string) string {
	return strings.NewReplacer("\t", " ", "\r\n", " ", "\n", " ", "\r", " ").Replace(v)
}

// showLabel は行の show の要約 (図解の種類 / next / 消す)。show を書いていない行は空。
func showLabel(line map[string]any) string {
	v, ok := line["show"]
	switch {
	case !ok:
		return ""
	case v == nil:
		return "消す"
	case v == show.Next:
		return "next"
	}
	if m, ok := v.(map[string]any); ok {
		return pyjson.Str(m["type"])
	}
	return ""
}
