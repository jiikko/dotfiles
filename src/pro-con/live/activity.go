package live

// カードの PG の活動 (応答の文と道具の呼び出し) を transcript から時刻の順に読む (issue 467)。
// 画面の詳細 (Backend.Activity) と `pro-con card log` が使う。🚨 読むだけ (transcript も記録も開いて読むだけ)。
//
// 読むのは pro-con の起動の記録 (sessions.json と、再開で入れ替わった前の session の sessions-retired.json) にある
// そのカードの session だけ (外の session の transcript は読まない。README の「pro-con が起動した session だけ」)。
// transcript は足されていくだけなので、読んだ位置を覚えて続きだけを読む (--follow と画面の読み直しを、大きさに比例させない)。

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"termsafe"

	"pro-con/backend"
)

// toolRunes は道具の呼び出しの要点を切る長さ (長い heredoc のコマンドで 1 行が画面を埋めない)。
const toolRunes = 300

// CardLog は 1 枚のカードの活動を、session をまたいで追う。Next を呼ぶたびに、前に読んだ後に足された分を返す。
// 1 つの goroutine から使う。
//
// 🚨 再開した session の transcript は、前の session の assistant のレコードを同じ uuid・時刻のまま写して始まる
// (2026-09-26 に実 transcript で確認: C-003 の再開後の session に、退いた session の 107 件が全部あった)。
// uuid で 1 度だけ出し、前の session から読む (写しではなく元の session の活動として出す)。
type CardLog struct {
	registry string
	find     func(sessionID string) (string, error)
	cardID   string
	session  string // カードの今の短い id (起動の記録にカードの無い古い行を拾うため)
	follow   []*follower
	seen     map[string]bool // 出したレコードの uuid
}

// follower は transcript 1 本の読んだ位置。
type follower struct {
	o    Owned
	path string
	off  int64
}

// NewCardLog は cardID のカードの活動を読む。registry は起動の記録 (sessions.json) のパス、find は session id から transcript を探す
// (FindTranscript)。session はカードの今の短い id。
func NewCardLog(registry string, find func(sessionID string) (string, error), cardID, session string) *CardLog {
	return &CardLog{registry: registry, find: find, cardID: cardID, session: session, seen: map[string]bool{}}
}

// Next は前に読んだ後に足された活動を古い順に返す (初回は全部)。まだ transcript の無い session は次に探し直す。
// 1 本の transcript が読めなくても、ほかの session は読んで返す (エラーは読めなかった分だけ)。
func (l *CardLog) Next() ([]backend.Activity, error) {
	reg, err := LoadRegistry(l.registry)
	if err != nil {
		return nil, err
	}
	retired, err := LoadRetired(l.registry)
	if err != nil {
		return nil, err
	}
	for _, o := range append(retired, reg...) {
		if l.mine(o) && !slices.ContainsFunc(l.follow, func(f *follower) bool { return f.o.SessionID == o.SessionID }) {
			l.follow = append(l.follow, &follower{o: o})
		}
	}
	// 起動の古い順に読む (写しの元の session が先に読まれ、活動は元の session の名で出る)
	slices.SortStableFunc(l.follow, func(a, b *follower) int { return a.o.StartedAt.Compare(b.o.StartedAt) })
	var out []backend.Activity
	var errs []error
	for _, f := range l.follow {
		if f.path == "" {
			p, err := l.find(f.o.SessionID)
			if err != nil {
				continue // まだ書かれていない (起動した直後)。次に探す
			}
			f.path = p
		}
		as, err := f.next(l.seen)
		if errors.Is(err, os.ErrNotExist) {
			f.path, f.off = "", 0 // 消えた・動いた: 次に探し直して頭から読む (読んだ分は uuid で重ねない)
		}
		if err != nil {
			errs = append(errs, err)
		}
		out = append(out, as...)
	}
	// 前の session の残りと今の session の出だしは時刻で混ざりうるので、全体を時刻で並べる (同じ時刻は読んだ順のまま)
	slices.SortStableFunc(out, func(a, b backend.Activity) int { return a.At.Compare(b.At) })
	return out, errors.Join(errs...)
}

// mine は起動の記録の行がこのカードの session か。カードの無い古い行は、短い id がカードの今の session と同じものだけ
// (短い id は別のカードと重なりうるので、カードのある行は短い id では拾わない。cardview の pgLog と同じ)。
func (l *CardLog) mine(o Owned) bool {
	if o.SessionID == "" {
		return false
	}
	if o.CardID != "" {
		return o.CardID == l.cardID
	}
	return l.session != "" && o.ID == l.session
}

// next は transcript の、前に読んだ位置の後の完全な行 (改行まで) を読む。書きかけの最後の行は次に読む。
// seen にある uuid のレコードは飛ばし、出したものを足す。transcript が読んだ位置より短くなっていたら (書き直された) 頭から読み直す。
func (f *follower) next(seen map[string]bool) ([]backend.Activity, error) {
	fh, err := os.Open(f.path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = fh.Close() }() // 読むだけなので閉じる失敗は結果に影響しない
	if st, err := fh.Stat(); err == nil && st.Size() < f.off {
		f.off = 0
	}
	if _, err := fh.Seek(f.off, io.SeekStart); err != nil {
		return nil, err
	}
	var out []backend.Activity
	// 🚨 bufio.Scanner にしない (transcript.go と同じ: 大きなツールの結果の行で読むのを黙って止める)
	br := bufio.NewReader(fh)
	for {
		line, err := br.ReadBytes('\n')
		if err != nil { // 改行の無い残り (書きかけ) は位置を進めずに次へ回す
			if errors.Is(err, io.EOF) {
				return out, nil
			}
			return out, err
		}
		f.off += int64(len(line))
		out = append(out, activities(line, f.o, seen)...)
	}
}

// activities は transcript の 1 行から、PG の応答の文と道具の呼び出しを取り出す (assistant のレコードだけ)。
// seen は出したレコードの uuid (nil なら重ねを見ない)。時刻の読めないレコードは出さない (並べる位置が無い)。
func activities(line []byte, o Owned, seen map[string]bool) []backend.Activity {
	var r record
	if json.Unmarshal(line, &r) != nil || r.Type != "assistant" || r.Message == nil {
		return nil
	}
	at, err := time.Parse(time.RFC3339Nano, r.Timestamp)
	if err != nil {
		return nil
	}
	if seen != nil && r.UUID != "" {
		if seen[r.UUID] {
			return nil // 再開した session に写された前の session のレコード
		}
		seen[r.UUID] = true
	}
	var out []backend.Activity
	say := func(text string) {
		if text = termsafe.PlainLine(oneLine(text)); text != "" {
			out = append(out, backend.Activity{At: at, Session: o.ID, Text: text})
		}
	}
	var s string
	if json.Unmarshal(r.Message.Content, &s) == nil {
		say(s)
		return out
	}
	for _, p := range parts(r.Message.Content) {
		switch p.Type {
		case "text":
			say(p.Text)
		case "tool_use":
			gist := termsafe.PlainLine(clip(oneLine(toolGist(p.Input, o.Cwd)), toolRunes))
			if gist == "" { // 引数から何も取れなくても、呼んだことは残す
				gist = "-"
			}
			out = append(out, backend.Activity{At: at, Session: o.ID, Tool: termsafe.PlainLine(p.Name), Text: gist})
		}
	}
	return out
}

// gistKeys は道具の引数のうち要点として出すもの (前の方ほど優先)。Bash はコマンド、Read / Edit / Write はファイル、Grep / Glob は型、
// Agent は説明。どれも無ければ引数の JSON をそのまま (切って) 出す。
var gistKeys = []string{"command", "file_path", "notebook_path", "pattern", "description", "url", "query", "skill", "prompt"}

// toolGist は道具の引数の要点。ファイルは PG の worktree (cwd) の中なら相対で出す。
func toolGist(raw json.RawMessage, cwd string) string {
	var in map[string]any
	if json.Unmarshal(raw, &in) != nil {
		return string(raw)
	}
	for _, k := range gistKeys {
		v, ok := in[k].(string)
		if !ok || strings.TrimSpace(v) == "" {
			continue
		}
		if (k == "file_path" || k == "notebook_path") && cwd != "" {
			if rel, err := filepath.Rel(cwd, v); err == nil && !strings.HasPrefix(rel, "..") {
				v = rel
			}
		}
		return v
	}
	if len(in) == 0 {
		return ""
	}
	return string(raw)
}
