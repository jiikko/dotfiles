// Package synth はエンジンへの要求 (audio_query / synthesis)・合成のキャッシュ・kana (読みの確認と英字の語の警告)。
package synth

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"zundamon-kaisetsu/internal/app"
	"zundamon-kaisetsu/internal/build"
	"zundamon-kaisetsu/internal/engine"
	"zundamon-kaisetsu/internal/outfile"
	"zundamon-kaisetsu/internal/paths"
	"zundamon-kaisetsu/internal/pyjson"
	"zundamon-kaisetsu/internal/script"
	"zundamon-kaisetsu/internal/textx"
	"zundamon-kaisetsu/internal/wav"
)

type speaker struct {
	Name   string `json:"name"`
	Styles []struct {
		Name string `json:"name"`
		ID   int64  `json:"id"`
	} `json:"styles"`
}

func fetchSpeakers(eng string) ([]speaker, error) {
	b, err := engine.Request(eng, "/speakers", nil, nil)
	if err != nil {
		return nil, err
	}
	var sp []speaker
	if err := json.Unmarshal(b, &sp); err != nil {
		return nil, app.Fail("/speakers の応答を読めない (%v)", err)
	}
	return sp, nil
}

func CmdSpeakers(env *app.Env) error {
	sp, err := fetchSpeakers(env.Engine)
	if err != nil {
		return err
	}
	for _, s := range sp {
		parts := make([]string, len(s.Styles))
		for i, st := range s.Styles {
			parts[i] = fmt.Sprintf("%s=%d", st.Name, st.ID)
		}
		fmt.Fprintf(env.Stdout, "%s: %s\n", s.Name, strings.Join(parts, ", "))
	}
	return nil
}

// audioQuery はエンジンの audio_query の結果。知らないフィールドも落とさず /synthesis と query.json へ運ぶため、
// 構造体ではなく map のまま、数は json.Number のまま持つ。
func audioQuery(eng, text string, styleID int64) (map[string]any, error) {
	b, err := engine.Request(eng, "/audio_query", url.Values{"text": {text}, "speaker": {strconv.FormatInt(styleID, 10)}}, []byte{})
	if err != nil {
		return nil, err
	}
	var q map[string]any
	if err := script.DecodeJSON(b, &q); err != nil {
		return nil, app.Fail("/audio_query の応答を読めない (%v)", err)
	}
	return q, nil
}

func queryKana(q map[string]any) string {
	if k, ok := q["kana"]; ok {
		return pyjson.Str(k)
	}
	return ""
}

// lineKana は台本の 1 行の読み。Spoken は音声にする文 (read か readings を当てた後)、Kana はエンジンの読み。
type lineKana struct {
	Who, Text, Spoken, Kana string
	StyleID                 int64
}

// scriptKana は台本の行の読みを 1 行ずつ each に渡す。synth 済みの行はキャッシュの読みを使い、無い行だけエンジンに問い合わせる
// (合成はしない)。1 行ずつ渡すのは、途中でエンジンが落ちても、それまでの行を出せるように
func scriptKana(env *app.Env, s *script.Script, each func(i int, l lineKana) error) error {
	wd := paths.WorkDir(s.Path)
	for i, line := range s.Lines {
		p, err := script.LineParams(s, line)
		if err != nil {
			return err
		}
		_, qp := script.CachePaths(wd, p)
		var kana string
		if b, err := os.ReadFile(qp); err == nil {
			var q map[string]any
			if err := script.DecodeJSON(b, &q); err != nil {
				return app.Fail("%s: 読めない (%v)", qp, err)
			}
			kana = queryKana(q)
		} else {
			q, err := audioQuery(env.Engine, p.Text, p.StyleID)
			if err != nil {
				return err
			}
			kana = queryKana(q)
		}
		if err := each(i, lineKana{Who: pyjson.Str(line["who"]), Text: pyjson.Str(line["text"]), Spoken: p.Text, Kana: kana, StyleID: p.StyleID}); err != nil {
			return err
		}
	}
	return nil
}

// CmdKana は文ごとに audio_query の読み (kana) を出す。read の候補 (カタカナ・ひらがな・英字のまま) を合成せずに比べる用。
//
// --script を付けると、台本の全行について 行番号 / 話者 / 字幕 / 読み を出す。音声を差し替えた行 (read か readings) には * を付ける。
// --check を付けると、代わりに読みの検査 (reading_check.go) の結果を出す。
func CmdKana(env *app.Env, texts []string, scriptArg, who string, styleID *int64, check bool) error {
	if check && scriptArg == "" {
		return app.Fail("kana の --check は --script と一緒に使う")
	}
	if scriptArg != "" {
		if len(texts) > 0 {
			return app.Fail("kana は --script か文のどちらか一方を渡す")
		}
		s, err := script.LoadScript(paths.ResolvePath(scriptArg), env)
		if err != nil {
			return err
		}
		if check {
			var lines []lineKana
			if err := scriptKana(env, s, func(_ int, l lineKana) error { lines = append(lines, l); return nil }); err != nil {
				return err
			}
			return checkReadings(env, lines, script.Acronyms(s))
		}
		return scriptKana(env, s, func(i int, l lineKana) error {
			mark := " "
			if l.Spoken != l.Text {
				mark = "*"
			}
			_, err := fmt.Fprintf(env.Stdout, "%d\t%s\t%s%s\t%s\n", i, l.Who, mark, l.Text, l.Kana)
			return err
		})
	}
	if len(texts) == 0 {
		return app.Fail("kana には読みを見たい文か --script <台本> を渡す")
	}
	def, _ := script.CastByKey(who)
	sid := def.StyleID
	if styleID != nil {
		sid = *styleID
	}
	for _, t := range texts {
		q, err := audioQuery(env.Engine, t, sid)
		if err != nil {
			return err
		}
		fmt.Fprintf(env.Stdout, "%s\t%s\n", t, queryKana(q))
	}
	return nil
}

// pyFloatJSON は合成の入力に入れる浮動小数を Python の json.dumps と同じ表記の json.Number にする。
func pyFloatJSON(f float64) json.Number { return json.Number(pyjson.FloatRepr(f)) }

func marshalNoEscape(v any) ([]byte, error) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(b.Bytes(), []byte("\n")), nil
}

func CmdSynth(env *app.Env, scriptArg string, force bool) error {
	s, err := script.LoadScript(paths.ResolvePath(scriptArg), env)
	if err != nil {
		return err
	}
	sp, err := fetchSpeakers(env.Engine)
	if err != nil {
		return err
	}
	styles := map[int64]string{}
	for _, x := range sp {
		for _, st := range x.Styles {
			styles[st.ID] = x.Name
		}
	}
	params := make([]script.Params, len(s.Lines))
	for i, line := range s.Lines {
		p, err := script.LineParams(s, line)
		if err != nil {
			return err
		}
		params[i] = p
		name, ok := styles[p.StyleID]
		if !ok {
			return app.Fail("lines[%d]: style_id=%d がエンジンに無い (speakers サブコマンドで調べる)", i, p.StyleID)
		}
		want, _ := script.CastByKey(pyjson.Str(line["who"]))
		if name != want.Name {
			return app.Fail("lines[%d]: style_id=%d は %s の声で、%s の声ではない", i, p.StyleID, name, want.Name)
		}
	}

	wd := paths.WorkDir(s.Path)
	if err := os.MkdirAll(wd, 0o755); err != nil {
		return app.Fail("%s: 作れない (%v)", wd, err)
	}
	made, cached := 0, 0
	for i, line := range s.Lines {
		p := params[i]
		wavPath, queryPath := script.CachePaths(wd, p)
		if paths.IsFile(wavPath) && paths.IsFile(queryPath) && !force {
			cached++
			continue
		}
		q, err := audioQuery(env.Engine, p.Text, p.StyleID)
		if err != nil {
			return err
		}
		q["speedScale"], q["pitchScale"] = pyFloatJSON(p.Speed), pyFloatJSON(p.Pitch)
		q["intonationScale"], q["volumeScale"] = pyFloatJSON(p.Intonation), pyFloatJSON(p.Volume)
		q["outputSamplingRate"], q["outputStereo"] = json.Number(strconv.Itoa(wav.SampleRate)), false
		body, err := marshalNoEscape(q)
		if err != nil {
			return app.Fail("audio_query の結果を書き出せない (%v)", err)
		}
		wavData, err := engine.Request(env.Engine, "/synthesis", url.Values{"speaker": {strconv.FormatInt(p.StyleID, 10)}}, body)
		if err != nil {
			return err
		}
		if _, _, err := wav.CheckBytes(wavData, fmt.Sprintf("lines[%d] の合成結果", i)); err != nil {
			return err // 壊れた wav をキャッシュに残さない
		}
		if err := outfile.WriteAtomic(queryPath, body); err != nil {
			return app.Fail("%s: 書けない (%v)", queryPath, err)
		}
		if err := outfile.WriteAtomic(wavPath, wavData); err != nil {
			return app.Fail("%s: 書けない (%v)", wavPath, err)
		}
		made++
		fmt.Fprintf(env.Stderr, "[%d/%d] %s: %s\n", i+1, len(s.Lines), pyjson.Str(line["who"]), textx.FirstRunes(p.Text, 30))
	}
	keep := map[string]bool{}
	for _, p := range params {
		w, q := script.CachePaths(wd, p)
		keep[filepath.Base(w)], keep[filepath.Base(q)] = true, true
	}
	entries, err := os.ReadDir(wd)
	if err != nil {
		return app.Fail("%s: 読めない (%v)", wd, err)
	}
	// 数えるのは合成のキャッシュ (通常ファイルの *.wav / *.query.json) だけ。build が置く mermaid/ (図のキャッシュ) などを
	// 「古いファイル」に数えると、案内に従って work/ ごと消し、図を描き直すことになる
	unused := 0
	for _, e := range entries {
		n := e.Name()
		if e.Type().IsRegular() && (strings.HasSuffix(n, ".wav") || strings.HasSuffix(n, ".query.json")) && !keep[n] {
			unused++
		}
	}
	msg := fmt.Sprintf("synth: 合成 %d / キャッシュ %d / 計 %d → %s", made, cached, len(s.Lines), wd)
	if unused > 0 {
		// work/ ごと消すよう案内しない (build が置く mermaid/ の図のキャッシュも消えて、描き直すことになる)
		msg += fmt.Sprintf(" (台本から外れた古いキャッシュ %d 件。%s/ の中の使われていない *.wav / *.query.json は消してよい)", unused, filepath.Base(wd))
	}
	fmt.Fprintln(env.Stderr, msg)
	// 字幕の速さは音声の長さで決まるので、build を待たずにここで知らせる。止めるのは build (書き出しの前) で、
	// synth は警告だけ: 直し方の pause_after は合成の鍵に入らず、synth をやり直さなくても build で効く
	// 時刻を出せない (キャッシュの wav が壊れている等) ときも synth は失敗にしない: 検査は案内で、壊れた wav は build が止める
	spans, duration, err := build.LineTimes(s)
	if err != nil {
		fmt.Fprintf(env.Stderr, "synth: 字幕の速さを確かめられない (%v)\n", err)
		return nil
	}
	// 丸めを build (Assemble の Duration) と揃える (境界の行で synth と build の判定が割れないように)
	if build.WarnFastCaptions(env.Stderr, "synth", build.CaptionLines(s, spans), pyjson.Round3(duration)) > 0 {
		fmt.Fprintln(env.Stderr, "synth: このままでは build が書き出す前に止まる (pause_after は synth し直さなくても build で効く)")
	}
	return nil
}
