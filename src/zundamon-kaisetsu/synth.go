package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type speaker struct {
	Name   string `json:"name"`
	Styles []struct {
		Name string `json:"name"`
		ID   int64  `json:"id"`
	} `json:"styles"`
}

func fetchSpeakers(engine string) ([]speaker, error) {
	b, err := engineRequest(engine, "/speakers", nil, nil)
	if err != nil {
		return nil, err
	}
	var sp []speaker
	if err := json.Unmarshal(b, &sp); err != nil {
		return nil, fail("/speakers の応答を読めない (%v)", err)
	}
	return sp, nil
}

func cmdSpeakers(env *Env) error {
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
func audioQuery(engine, text string, styleID int64) (map[string]any, error) {
	b, err := engineRequest(engine, "/audio_query", url.Values{"text": {text}, "speaker": {strconv.FormatInt(styleID, 10)}}, []byte{})
	if err != nil {
		return nil, err
	}
	var q map[string]any
	if err := decodeJSON(b, &q); err != nil {
		return nil, fail("/audio_query の応答を読めない (%v)", err)
	}
	return q, nil
}

func queryKana(q map[string]any) string {
	if k, ok := q["kana"]; ok {
		return pyStr(k)
	}
	return ""
}

// cmdKana は文ごとに audio_query の読み (kana) を出す。read の候補 (カタカナ・ひらがな・英字のまま) を合成せずに比べる用。
//
// --script を付けると、台本の全行について 行番号 / 話者 / 字幕 / 読み を出す。synth 済みの行はキャッシュの読みを使い、
// 無い行だけエンジンに問い合わせる (合成はしない)。音声を差し替えた行 (read か readings) には * を付ける。
func cmdKana(env *Env, texts []string, scriptArg, who string, styleID *int64) error {
	if scriptArg != "" {
		if len(texts) > 0 {
			return fail("kana は --script か文のどちらか一方を渡す")
		}
		s, err := loadScript(resolvePath(scriptArg), env)
		if err != nil {
			return err
		}
		wd := workDir(s.Path)
		for i, line := range s.Lines {
			p, err := lineParams(s, line)
			if err != nil {
				return err
			}
			_, qp := cachePaths(wd, p)
			var kana string
			if b, err := os.ReadFile(qp); err == nil {
				var q map[string]any
				if err := decodeJSON(b, &q); err != nil {
					return fail("%s: 読めない (%v)", qp, err)
				}
				kana = queryKana(q)
			} else {
				q, err := audioQuery(env.Engine, p.Text, p.StyleID)
				if err != nil {
					return err
				}
				kana = queryKana(q)
			}
			mark := " "
			if p.Text != pyStr(line["text"]) {
				mark = "*"
			}
			fmt.Fprintf(env.Stdout, "%d\t%s\t%s%s\t%s\n", i, pyStr(line["who"]), mark, pyStr(line["text"]), kana)
		}
		return nil
	}
	if len(texts) == 0 {
		return fail("kana には読みを見たい文か --script <台本> を渡す")
	}
	def, _ := castByKey(who)
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
func pyFloatJSON(f float64) json.Number { return json.Number(pyFloatRepr(f)) }

func marshalNoEscape(v any) ([]byte, error) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(b.Bytes(), []byte("\n")), nil
}

func cmdSynth(env *Env, scriptArg string, force bool) error {
	s, err := loadScript(resolvePath(scriptArg), env)
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
	params := make([]Params, len(s.Lines))
	for i, line := range s.Lines {
		p, err := lineParams(s, line)
		if err != nil {
			return err
		}
		params[i] = p
		name, ok := styles[p.StyleID]
		if !ok {
			return fail("lines[%d]: style_id=%d がエンジンに無い (speakers サブコマンドで調べる)", i, p.StyleID)
		}
		want, _ := castByKey(pyStr(line["who"]))
		if name != want.Name {
			return fail("lines[%d]: style_id=%d は %s の声で、%s の声ではない", i, p.StyleID, name, want.Name)
		}
	}

	wd := workDir(s.Path)
	if err := os.MkdirAll(wd, 0o755); err != nil {
		return fail("%s: 作れない (%v)", wd, err)
	}
	made, cached := 0, 0
	for i, line := range s.Lines {
		p := params[i]
		wavPath, queryPath := cachePaths(wd, p)
		if isFile(wavPath) && isFile(queryPath) && !force {
			cached++
			continue
		}
		q, err := audioQuery(env.Engine, p.Text, p.StyleID)
		if err != nil {
			return err
		}
		q["speedScale"], q["pitchScale"] = pyFloatJSON(p.Speed), pyFloatJSON(p.Pitch)
		q["intonationScale"], q["volumeScale"] = pyFloatJSON(p.Intonation), pyFloatJSON(p.Volume)
		q["outputSamplingRate"], q["outputStereo"] = json.Number(strconv.Itoa(sampleRate)), false
		body, err := marshalNoEscape(q)
		if err != nil {
			return fail("audio_query の結果を書き出せない (%v)", err)
		}
		wav, err := engineRequest(env.Engine, "/synthesis", url.Values{"speaker": {strconv.FormatInt(p.StyleID, 10)}}, body)
		if err != nil {
			return err
		}
		if _, _, err := checkWavBytes(wav, fmt.Sprintf("lines[%d] の合成結果", i)); err != nil {
			return err // 壊れた wav をキャッシュに残さない
		}
		if err := writeAtomic(queryPath, body); err != nil {
			return fail("%s: 書けない (%v)", queryPath, err)
		}
		if err := writeAtomic(wavPath, wav); err != nil {
			return fail("%s: 書けない (%v)", wavPath, err)
		}
		made++
		fmt.Fprintf(env.Stderr, "[%d/%d] %s: %s\n", i+1, len(s.Lines), pyStr(line["who"]), firstRunes(p.Text, 30))
	}
	keep := map[string]bool{}
	for _, p := range params {
		w, q := cachePaths(wd, p)
		keep[filepath.Base(w)], keep[filepath.Base(q)] = true, true
	}
	entries, err := os.ReadDir(wd)
	if err != nil {
		return fail("%s: 読めない (%v)", wd, err)
	}
	// 数えるのは合成のキャッシュ (通常ファイルの *.wav / *.query.json) だけ。build が置く mermaid/ (図のキャッシュ) などを
	// 「古いファイル」に数えると、案内に従って work/ ごと消し、図を描き直すことになる (issue 652)
	unused := 0
	for _, e := range entries {
		n := e.Name()
		if e.Type().IsRegular() && (strings.HasSuffix(n, ".wav") || strings.HasSuffix(n, ".query.json")) && !keep[n] {
			unused++
		}
	}
	msg := fmt.Sprintf("synth: 合成 %d / キャッシュ %d / 計 %d → %s", made, cached, len(s.Lines), wd)
	if unused > 0 {
		// work/ ごと消すよう案内しない (build が置く mermaid/ の図のキャッシュも消えて、描き直すことになる。issue 652)
		msg += fmt.Sprintf(" (台本から外れた古いキャッシュ %d 件。%s/ の中の使われていない *.wav / *.query.json は消してよい)", unused, filepath.Base(wd))
	}
	fmt.Fprintln(env.Stderr, msg)
	return nil
}
