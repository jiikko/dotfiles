package report

import (
	"fmt"
	"io"
	"path/filepath"
	"strconv"

	"dupscan/internal/group"
	"dupscan/internal/norm"
)

// compareShown は 1 つの組で出す explain -compare の argv の数 (先頭のメンバーと、残りのうち先頭からこの数まで)。
const compareShown = 3

// compareHints は、組のメンバーの違いを確かめる手を返す (-v で除外の 1 行の下に出す)。除外の理由を書く瞬間に、
// 確かめないまま「違う」と書かないため (読み比べただけで書いた「違う」が、実際は同じ本体だったことがある)。
// L1 は本体の局所の名前を番号にした形が同じ組 (norm の L1 の定義) なので argv を出さず、そう言う 1 行にする
// (explain -compare は本体の L1 の構造を比べるので、L1 の組では差が 0 になるだけ)。言い切らない所を 2 つ書く:
// 番号は綴りで振るので、局所の名前と同じ綴りの外側の名前も番号になる (`bytes := bytes.TrimSpace(b)` と `strings := strings.TrimSpace(s)`、
// Swift の `if let user`)。引数と receiver の型はハッシュに入らない (SuiteA と SuiteB の同じ本体のメソッドは L1 の組になる)。
// ほかの段は、行で指せる最初のメンバーと他のメンバーを explain -compare で比べる argv。メンバーは行で指し、その行で explain が
// 同じ単位を選ぶ (group.AtLine) ときだけ出す。名前で指さないのは、クロージャに名前が無く、同じファイルの同名 (多重定義) で止まるため。
// パスは root を付けた絶対パス (explain は cwd から解く。repo の相対パスだと、cwd が別の checkout なら同じ名前の別のファイルを比べる)。
// entries が nil なら argv を出さない (gate の head が commit のとき。explain は作業ツリーを読む)。
func compareHints(root string, lv norm.Level, members, entries []*group.Entry) []string {
	if len(members) < 2 {
		return nil
	}
	if lv == norm.L1 {
		return []string{"違い: 本体で局所の名前として番号にした綴りだけ。リテラルと、番号にしなかった名前は全員同じ (L1。局所の名前と同じ綴りの外側の名前 (package・プロパティ) も番号になる。引数と receiver の型は比べていない)"}
	}
	if entries == nil {
		return nil
	}
	var sels []string
	unpointable := 0
	for _, m := range members {
		if sel, ok := lineSelector(root, m, entries); ok {
			sels = append(sels, sel)
		} else {
			unpointable++
		}
	}
	var out []string
	if len(sels) >= 2 {
		for i, sel := range sels[1:] {
			if i == compareShown {
				out = append(out, fmt.Sprintf("… 他 %d 個も同じ形で比べる", len(sels)-1-compareShown))
				break
			}
			out = append(out, "違いを見る: dupscan explain -compare "+sel+" "+sels[0])
		}
	}
	if unpointable > 0 {
		out = append(out, fmt.Sprintf("行で指せないメンバー %d 個 (その行で explain が別の単位を選ぶ) は比べる argv を出さない。範囲で Read して比べる", unpointable))
	}
	return out
}

// lineSelector は e を explain の `<file>:<行>` で指す引数 (root を付けた絶対パス)。その行で別の単位が選ばれるなら ok=false。
func lineSelector(root string, e *group.Entry, entries []*group.Entry) (string, bool) {
	u := e.Unit
	got := group.AtLine(entries, u.File, u.Line)
	if got == nil || got.Unit.Key != u.Key || got.Unit.Line != u.Line || got.Unit.Col != u.Col {
		return "", false
	}
	return shellQuote(filepath.Join(root, u.File)) + ":" + strconv.Itoa(u.Line), true
}

func writeCompare(w io.Writer, hints []string) {
	for _, h := range hints {
		fmt.Fprintf(w, "  %s\n", h)
	}
}
