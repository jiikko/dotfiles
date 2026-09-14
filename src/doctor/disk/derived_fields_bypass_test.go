package disk

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// Report.Total / Result.Size は導出値なので、**所有者パッケージの外**からは
// WithResults / WithItems を通してしか差し替えない (issue 372)。
//
// なぜ機械で縛るか: `rep.Results = xs` と直接書いても build もテストも通り、
// **合計だけが古いまま残る** (画面上部の「解放可能 N GB」が行と食い違う / 消したのに減らない)。
// 寄せる前の glogx 側 3 箇所がまさにその形だった。「書く人が覚えているか」に依存させない
// (glogx の issues_rows_setter_test.go と同じ形)。
//
// 🚨 **脅威モデルと射程** (adversarial-review-own-safeguards §8):
//   - 止めるのは「うっかり `rep.Results = xs` / `r.Items = its` と直接書く」典型形だけ。
//     意図的な迂回 (reflect / unsafe) は review の責務
//   - 射程は **所有者パッケージ (disk) の外** かつ **doctor/disk を import しているファイル**。
//     disk 自身を射程に入れない理由は 2 つ: (a) WithResults / WithItems / Scan の実装そのものが
//     導出フィールドを書く唯一の出典であること (b) delete_test.go が
//     「Size と Items を故意に食い違わせる」fixture を持っており (1TB と申告する / Items だけ
//     空にする)、そこは不整合を作ることがテストの主張そのものであること
//   - 見るのは 3 種:
//     ① `<disk.Report>.Results = / .Total =` と `<disk.Result>.Items = / .Size =`
//        (複合代入 `+=` と `x++`、`*p` / `xs[i]` / フィールド経由の別名を含む)
//     ② 要素まるごとの差し替え `<Report>.Results[i] = …` / `<Result>.Items[i] = …`
//        (中身が正しくても Total / Size は引き直されない。`= r.WithItems(xs)` と書けるので
//        「所有者 API を通している」ように読めるのが厄介)。
//        🚨 **セレクタから直接書く形だけ**。`rs := rep.Results; rs[0] = x` のような別名経由は
//        検出しない — ローカルに組んだ `[]disk.Result` (実測 75 箇所。`out[i] = r` は
//        正当な構築) と区別できず、広げると誤検出になる
//     ③ **production の**複合リテラル `disk.Report{… Results: xs}` / `disk.Result{… Items: its}`。
//        要素の型を省略した `[]disk.Result{{Items: its}}` / `[N]disk.Result{…}` /
//        `map[K]disk.Result{…}` も外側の型から降ろして見る。
//        キー無しリテラル (`disk.Report{rs, 0, …}`) は govet の composites が別に止める
//   - **検出しない**: reflect / json.Unmarshal 経由 (保存した snapshot の復元は Results と Total を
//     同時に埋めるので正しい) / 関数へ渡して中で書く形 (`copy(rep.Results, xs)` を含む) /
//     ローカル変数が call の戻り値のとき (`rep := build()` は型を追わない) / `new(disk.Report)` /
//     import に別名を付けたうえで**さらに**その別名を再束縛する形 / 型エイリアス経由 /
//     埋め込みフィールド (`type wrap struct{ disk.Report }` の `w.Results`) /
//     パッケージレベルの `var f = func(rep *disk.Report){…}` (関数リテラルの本体は
//     FuncDecl の中にあるものだけ見る。`var rep disk.Report` は見る) /
//     型スイッチの束縛 / `map[string]*disk.Report` や `[]*disk.Report` の要素 /
//     ②の別名経由 (上記) と `p := &rep.Results[0]; *p = x`。
//     これらは review の責務
//   - **テストの複合リテラルは射程外**。fixture が Total / Size を埋めないのは
//     「まだ計算していない」であって退行ではない (所有者パッケージ内の delete_test.go を
//     射程外にしているのと同じ理由)。実測 2026-09-14 でテスト側 137 箇所 / production 1 箇所
//   - フィールド名の表は名前だけで型を決めるので、絞る: **production からだけ集める**
//     (テストの table のフィールド名まで採ると、テストを 1 つ改名しただけで
//     この検査が「衝突」と誤診して赤くなる) / **短い名前 (4 文字未満) は採らない** /
//     **名前が衝突したら落とす**。誤検出を出さない側へ倒しているぶん、
//     採らなかった名前は**盲点**になる。黙って増減しないよう、下で
//     (a) 実在する owner (`Disk` / `diskRep` / `diskResults`) が表に居ること
//     (b) 衝突で落ちた名前が 0 件 (c) 短くて採らなかった名前が既知の一覧と一致すること、を assert する。
//     🚨 **今の盲点は 2 つ** (`rep` / `r` = `doctorDiskEvent.{rep,r}`。走査 goroutine から
//     model へ Report / Result を運ぶ production の口) なので、`msg.ev.rep.Results = …` は
//     素通りする。採ると `h.r.Size = 0` のような無関係な型への書き込みを誤検出するため、
//     盲点として引き受ける
//   - **見逃す形 (スコープ)**: `owners` は関数単位で、ブロックスコープを見ない。
//     `{ rep := len(xs) }` のようにブロック内で owner 名を `:=` でシャドウすると、
//     **そのブロックを抜けた後の本物の書き込みまで見えなくなる**。
//     引数 / `var` / 型を追えない `:=` / range の束縛は消すので誤検出側には倒れないが、
//     この見逃しは残る (スコープを厳密に追うには go/types が要る)
//   - 🚨 **この射程は実装後に実物と突き合わせて 3 度直したもの**。初版は
//     `disk.EntryOutcome.Items` と `doctorDiskCache.Total` を**誤検出する**形だった。
//     2 周目で「別名変数経由は検出しない」という**宣言が実装と逆**だったこと、②③が
//     抜けていたこと、`r` / `sel` を表に採ったせいで**無関係なコードを落とす誤検出**を
//     作れること、`var rep svc.Report` の再宣言を跨いで誤分類していたことが分かった。
//     3 周目で **CI の paths を見る assert 自身が false green** だったこと
//     (コメントアウトも `paths-ignore` への反転も緑)、引数と range の束縛に
//     同じ誤検出が残っていたこと、`sel` (テスト由来) が無関係な改名で赤を出すことが分かった
//
// 🚨 **CI の配線** (ここが 2 段構え。どちらが欠けても検査は「在るのに走らない」):
//  1. `.github/workflows/src_doctor.yml` の paths に `src/glogx/**` がある
//     (無いと「glogx だけ変えた push」で workflow 自体が起動しない)
//  2. `src/doctor/Makefile` の `test-derived-fields` が `-count=1` で回す
//     (この検査は module の外を読むので **go test のキャッシュキーに入力が 1 つも入らない**。
//     付けないと `ok (cached)` を返し続ける。しかも最も当たりやすいのは 1 が守りたい
//     「glogx だけ変えた push」で、doctor のソースが変わらないため必ずキャッシュに当たる)
//
// 1 は下の workflowPaths が検査する。🚨 **その脅威モデルは「うっかり paths を消す /
// コメントアウトする」典型形だけ**を止めること (§8 の stopping rule)。手書きの YAML anchor、
// フロー形式、`branches` の絞り込み、`jobs.<id>.if: false` のような「CI を無効化する
// 別の書き方」は原理的に無限にあるので**追わない** — 読めない書き方に出会ったら
// 「読めなかった」と言って落ちる (在るとも無いとも言わない) までが射程で、
// その先は review の責務。2 は Makefile 側のコメントが正本。
//
// scanDerivedWrites は 1 ファイル分の違反と「型を解決できた参照の数」を返す
// (本走査と canary の共通経路)。
//
// 🚨 canary は**この関数を通す**こと。式をコピーして別に書くと、canary は「コピーした
// ロジック」を検査するだけで本走査の破損を検出しない。

type ownerKind int

const (
	ownerNone ownerKind = iota
	ownerReport
	ownerResult
	ownerResultSlice
)

// ownerFields は「disk.Report / disk.Result 型で宣言された struct フィールド名」の表。
// `sn.Disk.Results = …` / `v.diskRep.Results = …` のような**フィールド経由**の owner を
// 解決するために要る (型解決を go/types に頼らない代わりの近似)。
type ownerFields map[string]ownerKind

// diskScanner は 1 ファイルを見るための文脈 (disk import のローカル名 + フィールド表)。
type diskScanner struct {
	diskName string // このファイルでの doctor/disk のローカル名 (既定 "disk")
	fields   ownerFields
	inTest   bool // _test.go か (複合リテラルの検査を production だけに掛けるため)
}

// diskLocalName は doctor/disk の import 名を返す (import していなければ "")。
func diskLocalName(file *ast.File) string {
	for _, im := range file.Imports {
		p, err := strconv.Unquote(im.Path.Value)
		if err != nil || p != "doctor/disk" {
			continue
		}
		if im.Name != nil {
			if im.Name.Name == "_" || im.Name.Name == "." {
				return "" // 検出しない形 (射程外)
			}
			return im.Name.Name
		}
		return "disk"
	}
	return ""
}

// typeKind は型式が disk.Report / disk.Result / []disk.Result のどれかを返す。
func (s diskScanner) typeKind(e ast.Expr) ownerKind {
	switch t := e.(type) {
	case *ast.StarExpr:
		return s.typeKind(t.X)
	case *ast.ParenExpr:
		return s.typeKind(t.X)
	case *ast.SelectorExpr:
		if id, ok := t.X.(*ast.Ident); ok && id.Name == s.diskName {
			switch t.Sel.Name {
			case "Report":
				return ownerReport
			case "Result":
				return ownerResult
			}
		}
	case *ast.ArrayType:
		if t.Len == nil && s.typeKind(t.Elt) == ownerResult {
			return ownerResultSlice
		}
	}
	return ownerNone
}

// elemOwnerKind は「要素 (map なら値) の owner 種別」。
// `[]disk.Result` / `[N]disk.Report` / `map[K]disk.Result` / それらのポインタ版を受ける。
// 要素の型を省略した複合リテラル (`[]disk.Result{{Items: its}}`) を外側から解くのに使う。
func (s diskScanner) elemOwnerKind(e ast.Expr) ownerKind {
	switch t := e.(type) {
	case *ast.ArrayType:
		return s.typeKind(t.Elt)
	case *ast.MapType:
		return s.typeKind(t.Value)
	}
	return ownerNone
}

// ownerFieldMinLen は owner としてフィールド名を採用する最短の長さ。
//
// 🚨 **短い名前を採らない**。フィールド表は名前だけで型を決める (kindOf のセレクタ枝) ので、
// `r` / `sel` のようなありふれた名前を採ると **無関係な型の `h.r.Size = 0` を違反と報告する**。
// 実測 2026-09-14 (敵対レビュー 2 周目): `r` は `doctorDiskEvent.r *disk.Result`、
// `sel` は glogx のテストの table 構造体 `sel []disk.Result` 由来で表に入っており、
// glogx のどこかに `r` という名前のフィールドを 1 つ足すだけで **doctor のこの検査が落ちる**
// 状態だった (相手が consumer ファイルなら衝突 assert で、非 consumer なら誤検出で、
// どちらに置いても赤くなる = 逃げ場が無い)。誤検出を出さない側へ倒す。
const ownerFieldMinLen = 4

// collectOwnerFields は struct のフィールド宣言を表へ足す。
// 同じ名前が別の型でも宣言されていたら (衝突) owner から落とす。短すぎる名前は採らない。
func (s diskScanner) collectOwnerFields(file *ast.File, into ownerFields, conflict, short map[string]bool) {
	ast.Inspect(file, func(n ast.Node) bool {
		st, ok := n.(*ast.StructType)
		if !ok || st.Fields == nil {
			return true
		}
		for _, f := range st.Fields.List {
			k := s.typeKind(f.Type)
			for _, name := range f.Names {
				if k == ownerNone {
					conflict[name.Name] = true
					continue
				}
				if len(name.Name) < ownerFieldMinLen {
					short[name.Name] = true
					continue
				}
				if prev, seen := into[name.Name]; seen && prev != k {
					conflict[name.Name] = true
					continue
				}
				into[name.Name] = k
			}
		}
		return true
	})
}

// kindOf は式が指す値が disk.Report / disk.Result かを近似で返す。
func (s diskScanner) kindOf(e ast.Expr, owners map[string]ownerKind) ownerKind {
	switch t := e.(type) {
	case *ast.ParenExpr:
		return s.kindOf(t.X, owners)
	case *ast.StarExpr: // (*p).Results = … / *p = …
		return s.kindOf(t.X, owners)
	case *ast.Ident:
		return owners[t.Name]
	case *ast.SelectorExpr:
		// Report.Results は []disk.Result なので、フィールド表を待たずにここで解決する
		// (Results は disk 内で宣言されており、射程外なので表には載らない)
		if t.Sel.Name == "Results" && s.kindOf(t.X, owners) == ownerReport {
			return ownerResultSlice
		}
		return s.fields[t.Sel.Name]
	case *ast.IndexExpr: // rep.Results[i].Items = … / rs[i].Size = …
		if s.kindOf(t.X, owners) == ownerResultSlice {
			return ownerResult
		}
	}
	return ownerNone
}

// rhsKind は右辺が disk.Report / disk.Result を作る式かを返す
// (`disk.Report{…}` / `&disk.Result{…}` / 既に owner と分かっている式)。
func (s diskScanner) rhsKind(e ast.Expr, owners map[string]ownerKind) ownerKind {
	switch t := e.(type) {
	case *ast.UnaryExpr: // &disk.Report{…} / &rep
		return s.rhsKind(t.X, owners)
	case *ast.ParenExpr:
		return s.rhsKind(t.X, owners)
	case *ast.CompositeLit:
		return s.typeKind(t.Type)
	}
	// 🚨 それ以外 (ident / セレクタ / `*p` / `xs[i]`) は **kindOf と同じ規則**で解く。
	// ここを別実装にすると「所有者から値を取り出してローカルへ束ねる」形だけが落ちる:
	// `rep := *v.diskRep` (StarExpr) / `p := &rep.Results[0]` (IndexExpr) /
	// `rs := rep.Results` (Results 特例) の 3 つが、rhsKind に枝が無いせいで
	// すべて未検出だった (敵対レビュー 2026-09-14 P1-1)。実コードに現役の形
	// (`src/glogx/doctor_delete.go` の `next := *ev.prog`) なので、統合して塞ぐ。
	return s.kindOf(e, owners)
}

// workflowPaths は workflow の `on.<trigger>.paths` の要素を trigger ごとに返す。
//
// 🚨 YAML パーサは持ち込まず、インデントで読む (doctor は依存を増やさない方針)。
// 代わりに **想定外の書き方は error にする** (paths-ignore / 未知のキー) — 「読めなかった」を
// 緑にしないため。コメント行は捨てる (コメントアウトを「在る」と読まない)。
func workflowPaths(text string) (map[string][]string, error) {
	out := map[string][]string{}
	inOn, trigger, inPaths := false, "", false
	for _, raw := range strings.Split(text, "\n") {
		if strings.Contains(raw, "\t") {
			return nil, errors.New("タブインデントがある (この読み取りはスペース前提)")
		}
		line := strings.TrimRight(raw, " \t\r")
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		// 行末コメントを落とす (`workflow_dispatch:  # 手動再実行用` のような形がある)
		if i := strings.Index(trimmed, " #"); i >= 0 {
			trimmed = strings.TrimSpace(trimmed[:i])
			if trimmed == "" {
				continue
			}
		}
		indent := len(line) - len(strings.TrimLeft(line, " "))
		switch {
		case indent == 0:
			// 🚨 **`on:` に錨を打つ**。打たないと `jobs:` の下の `push:` を trigger として
			// 読んでしまい、`on:` に paths が 1 つも無くても緑になる (4 周目 P2-3 が実測)
			inOn = strings.TrimSuffix(trimmed, ":") == "on" || trimmed == `"on":`
			trigger, inPaths = "", false
		case indent == 2 && inOn:
			trigger, inPaths = strings.TrimSuffix(trimmed, ":"), false
			if !strings.HasSuffix(trimmed, ":") {
				return nil, fmt.Errorf("on の下に想定外の書き方がある: %q", trimmed)
			}
		case indent == 4 && trigger != "":
			key, rest, hasValue := strings.Cut(trimmed, ":")
			switch key {
			case "paths-ignore", "branches-ignore":
				return nil, fmt.Errorf("%s に %s がある (paths / branches とは意味が逆)", trigger, key)
			case "paths":
				// フロー形式 (`paths: ['a', 'b']`) やアンカー参照は読めないので error にする
				// (読めなかったものを「在る」とも「無い」とも言わない)
				if hasValue && strings.TrimSpace(rest) != "" {
					return nil, fmt.Errorf("%s の paths がブロック形式でない: %q", trigger, trimmed)
				}
				inPaths = true
			default:
				inPaths = false
			}
		case indent >= 6 && inPaths:
			if !strings.HasPrefix(trimmed, "- ") {
				return nil, fmt.Errorf("%s の paths に想定外の行がある: %q", trigger, trimmed)
			}
			v := strings.TrimSpace(strings.TrimPrefix(trimmed, "- "))
			out[trigger] = append(out[trigger], strings.Trim(v, `'"`))
		}
	}
	if !inOnSeen(text) {
		return nil, errors.New("トップレベルの on: が見つからない")
	}
	return out, nil
}

// inOnSeen はトップレベルに `on:` があるか (workflowPaths の前提の確認)。
func inOnSeen(text string) bool {
	for _, raw := range strings.Split(text, "\n") {
		t := strings.TrimRight(raw, " \t\r")
		if t == "on:" || t == `"on":` {
			return true
		}
	}
	return false
}

// derivedOf は「そのフィールド名が、どの owner の導出フィールドか」を返す。
func derivedOf(field string) ownerKind {
	switch field {
	case "Results", "Total":
		return ownerReport
	case "Items", "Size":
		return ownerResult
	}
	return ownerNone
}

func (s diskScanner) scanFunc(fset *token.FileSet, path string, fn *ast.FuncDecl, pkgOwners map[string]ownerKind, offenders *[]string, resolved *int) {
	owners := map[string]ownerKind{}
	// パッケージレベルの `var rep disk.Report` も owner に数える (敵対レビュー 2 周目 P2-3)。
	// 走査は FuncDecl の本体だけなので、これが無いと最も素朴な `zzF.Results = rs` が素通りする
	for k, v := range pkgOwners {
		owners[k] = v
	}
	addField := func(fl *ast.FieldList) {
		if fl == nil {
			return
		}
		for _, f := range fl.List {
			k := s.typeKind(f.Type)
			for _, n := range f.Names {
				// 🚨 disk 型でない引数は**束縛を消す**。消さないと、同じ関数の中で
				// `func(r *disk.Result)` のリテラルを書いた後に `for _, r := range rows`
				// (別の型) を回すだけで、`r.Size = 0` を違反と報告する
				// (敵対レビュー 3 周目 P2-1 が実測。的になる同名フィールドは実在する)
				if k == ownerNone {
					delete(owners, n.Name)
					continue
				}
				owners[n.Name] = k
			}
		}
	}
	// 🚨 レシーバは見ない。Go は他パッケージの型にメソッドを定義できないので、
	// 所有者パッケージの外でレシーバの型が disk.Report / disk.Result になることは
	// **原理的に無い**。前例 (glogx の issuesView はローカル型) から写して残っていた枝で、
	// 変異を当てても緑のままだった (敵対レビュー 2026-09-14 P2-1 の M-e)。
	if fn.Type != nil {
		addField(fn.Type.Params)
		addField(fn.Type.Results)
	}

	report := func(pos token.Pos, what string) {
		p := fset.Position(pos)
		*offenders = append(*offenders,
			filepath.ToSlash(path)+":"+strconv.Itoa(p.Line)+" ("+fn.Name.Name+", "+what+")")
	}
	check := func(lhs ast.Expr, what string) {
		switch t := lhs.(type) {
		case *ast.SelectorExpr:
			want := derivedOf(t.Sel.Name)
			if want != ownerNone && s.kindOf(t.X, owners) == want {
				report(t.Pos(), what+" "+t.Sel.Name)
			}
		case *ast.IndexExpr:
			// 🚨 `rep.Results[i] = r` / `r.Items[i] = it` の**要素まるごと差し替え**。
			// 中身が正しくても Total / Size は引き直されない。しかも
			// `rep.Results[i] = r.WithItems(xs)` のように所有者 API を呼んでいる形で書けるので、
			// コードは「通している」ように読める (敵対レビュー 2026-09-14 P1-3)。
			// glogx の前例が `v.rows[i] = x` を見ているのと同じ枠。
			sel, ok := t.X.(*ast.SelectorExpr)
			if !ok {
				return
			}
			switch {
			case sel.Sel.Name == "Results" && s.kindOf(sel.X, owners) == ownerReport:
				report(t.Pos(), what+" Results[i] (要素の差し替え)")
			case sel.Sel.Name == "Items" && s.kindOf(sel.X, owners) == ownerResult:
				report(t.Pos(), what+" Items[i] (要素の差し替え)")
			}
		}
	}

	ast.Inspect(fn.Body, func(n ast.Node) bool {
		// 関数リテラルの引数も owner に数える (コールバックで *disk.Report を受ける形)。
		// FuncDecl の引数だけ数えて FuncLit を数えないのは非対称 (敵対レビュー P3)
		if fl, ok := n.(*ast.FuncLit); ok && fl.Type != nil {
			addField(fl.Type.Params)
		}
		// 🚨 range の束縛は消す。`for _, r := range rep.Results` の `r` は**値のコピー**なので
		// そこへ書いても不変条件は壊れない (違反ではない) 一方、別の型のスライスを同名で
		// 回されると誤検出になる。どちらの向きにも倒れないよう、束縛を落とす
		// 🚨 `:=` の range だけ (新しい束縛を作る形)。`for _, rep = range …` (ASSIGN) は
		// 既存の変数を回すので型が変わらず、消すと検出力だけが落ちる (4 周目 P3-2)
		if rs, ok := n.(*ast.RangeStmt); ok && rs.Tok == token.DEFINE {
			for _, e := range []ast.Expr{rs.Key, rs.Value} {
				if id, ok := e.(*ast.Ident); ok {
					delete(owners, id.Name)
				}
			}
		}
		// 🚨 production の複合リテラルで導出フィールドを埋める形
		// (`disk.Report{ScannedAt: …, Results: results}`)。Total が 0 のまま残る。
		// **テストは射程外**: 所有者パッケージ内の delete_test.go と同じで、
		// fixture が Total / Size を埋めないのは「まだ計算していない」であって退行ではない
		// (実測 2026-09-14: テスト側に 137 箇所、production は 1 箇所だけで、
		// その 1 箇所 `doctor_view.go` は既に `.WithResults(results)` を付けている。
		// この検査はその `.WithResults` を剥がす変更を止めるためにある)
		if cl, ok := n.(*ast.CompositeLit); ok && !s.inTest {
			keys := func(lit *ast.CompositeLit, k ownerKind) {
				for _, elt := range lit.Elts {
					kv, ok := elt.(*ast.KeyValueExpr)
					if !ok {
						continue
					}
					if id, ok := kv.Key.(*ast.Ident); ok && derivedOf(id.Name) == k {
						report(kv.Pos(), "複合リテラル "+id.Name)
					}
				}
			}
			if k := s.typeKind(cl.Type); k == ownerReport || k == ownerResult {
				keys(cl, k)
			}
			// 🚨 `[]disk.Result{{Items: its}}` は**要素の型が省略される**ので `Type == nil` に
			// なり、要素だけ見ても disk.Result と分からない。外側の型から降ろす
			// (敵対レビュー 2 周目 P1-2。Size が 0 のまま残る最も自然な書き方だった)。
			// slice / 固定長配列 / map の値も同じ (3 周目 P3-1)
			if ek := s.elemOwnerKind(cl.Type); ek != ownerNone {
				for _, elt := range cl.Elts {
					inner := elt
					if kv, ok := elt.(*ast.KeyValueExpr); ok { // map[...]disk.Result{"a": {…}}
						inner = kv.Value
					}
					if lit, ok := inner.(*ast.CompositeLit); ok {
						keys(lit, ek)
					}
				}
			}
		}
		// var rep disk.Report / var rs []disk.Result
		if vs, ok := n.(*ast.ValueSpec); ok {
			k := s.typeKind(vs.Type)
			switch {
			case k != ownerNone:
				for _, name := range vs.Names {
					owners[name.Name] = k
				}
			default:
				// 🚨 `var rep svc.Report` — **別の型での再宣言では前の束縛を消す**。
				// 消さないと、同じ関数の別クロージャの `rep` を disk.Report と誤分類したまま
				// 残る。実測 2026-09-14 (敵対レビュー 2 周目 P2-1): glogx の `start()` が
				// まさにこの形で、`svc.Report` に `Total` という名前のフィールドが
				// 足された瞬間に誤検出が出る状態だった (今日出ていないのは偶然)。
				// 🚨 型を明示した再宣言のために別の枝を書いたが、**このループが同じ削除をする**
				// (値が無ければ rk は ownerNone、値があっても disk 型でなければ同じ) ので
				// 変異 M16 が等価変異になった。枝を消してここへ寄せた
				for i, name := range vs.Names {
					rk := ownerNone
					if i < len(vs.Values) {
						rk = s.rhsKind(vs.Values[i], owners)
					}
					if rk == ownerNone {
						delete(owners, name.Name)
						continue
					}
					owners[name.Name] = rk
				}
			}
		}
		// 型が解決できた参照を数える (読み書きの別を問わない)。
		// 🚨 違反を直し切ると違反数は 0 になるので、**判定が生きている証拠**をここで稼ぐ
		// (0 件 = 緑 の false green を塞ぐ。verify-execution-not-just-exit-code の「canary」)。
		if sel, ok := n.(*ast.SelectorExpr); ok && s.kindOf(sel.X, owners) != ownerNone {
			*resolved++
		}
		if inc, ok := n.(*ast.IncDecStmt); ok {
			check(inc.X, "インクリメント")
		}
		as, ok := n.(*ast.AssignStmt)
		if !ok {
			return true
		}
		// rep := disk.Report{…} / r := other (owner)
		for i, lhs := range as.Lhs {
			id, ok := lhs.(*ast.Ident)
			if !ok || i >= len(as.Rhs) {
				continue
			}
			rk := s.rhsKind(as.Rhs[i], owners)
			switch {
			case rk != ownerNone:
				owners[id.Name] = rk
			case as.Tok == token.DEFINE:
				// 🚨 `rep := <型を追えない式>` は**新しい変数**なので前の束縛を持ち越さない。
				// `rep = rep.WithResults(x)` (ASSIGN) は Go では型が変わらないので消さない
				// (消すと、寄せた直後の書き込みが検出できなくなる)
				delete(owners, id.Name)
			}
		}
		what := "直接代入"
		if as.Tok != token.ASSIGN && as.Tok != token.DEFINE {
			what = "複合代入"
		}
		for _, lhs := range as.Lhs {
			check(lhs, what)
		}
		return true
	})
}

// scanDerivedWrites は 1 ファイルを走査する (本走査と canary の共通経路)。
func scanDerivedWrites(fset *token.FileSet, path string, file *ast.File, fields ownerFields) (offenders []string, resolved int) {
	name := diskLocalName(file)
	if name == "" {
		return nil, 0
	}
	s := diskScanner{diskName: name, fields: fields, inTest: strings.HasSuffix(path, "_test.go")}
	// パッケージレベルの `var rep disk.Report` を先に集める (敵対レビュー 2 周目 P2-3)
	pkgOwners := map[string]ownerKind{}
	for _, decl := range file.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.VAR {
			continue
		}
		for _, spec := range gd.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for i, nm := range vs.Names {
				k := s.typeKind(vs.Type)
				if k == ownerNone && i < len(vs.Values) {
					k = s.rhsKind(vs.Values[i], pkgOwners)
				}
				if k != ownerNone {
					pkgOwners[nm.Name] = k
				}
			}
		}
	}
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		s.scanFunc(fset, path, fn, pkgOwners, &offenders, &resolved)
	}
	return offenders, resolved
}

const derivedCanarySrc = `package zz

import "doctor/disk"

type holder struct {
	Disk       disk.Report
	diskRep    *disk.Report
	resultList []disk.Result
	rs         []disk.Result // 短すぎる名前 (owner に採ってはいけない側)
}

// 別の型だが同じフィールド名を持つ (誤検出してはいけない側)
type cache struct {
	Total int64
	Items []string
}

// 要素の差し替えの型ガードを検査するための別の型 (誤検出してはいけない側)
type otherList struct {
	Results []cache
	Items   []string
}

// パッケージレベルの owner (走査は FuncDecl の本体だけなので、別に集めないと見えない)
var pkgRep disk.Report

func canaryReport(rep disk.Report, h *holder) {
	rep.Results = nil                 // 1 直接代入
	rep.Total = 0                     // 2 直接代入
	rep.Total += 1                    // 3 複合代入
	rep.Total++                       // 4 インクリメント
	h.Disk.Results = nil              // 5 フィールド経由
	h.diskRep.Total = 0               // 6 ポインタのフィールド経由
	local := disk.Report{}
	local.Results = nil               // 7 複合リテラル由来のローカル変数
}

func canaryResult(r disk.Result, h *holder, rep disk.Report) {
	r.Items = nil                     // 8 直接代入
	r.Size = 0                        // 9 直接代入
	h.resultList[0].Items = nil       // 10 スライス要素経由
	rep.Results[1].Size = 0           // 11 Report.Results の要素経由
	var v disk.Result
	v.Size = 0                        // 12 var 宣言経由
}

// 敵対レビュー 2026-09-14 で「すり抜ける」と実測された形 (P1-1 / P1-3 / P3)。
// 🚨 canary に無い枝は変異を当てても緑のままなので、塞いだ形はここへ足すこと。
func canaryAliases(h *holder, rep disk.Report, r disk.Result) {
	(*h.diskRep).Total = 0            // 13 デリファレンスして書く
	cp := *h.diskRep
	cp.Results = nil                  // 14 デリファレンス・コピーへ書く (rhsKind の StarExpr)
	p := h.diskRep
	p.Results = nil                   // 15 ポインタの別名 (rhsKind -> kindOf のセレクタ)
	pi := &h.resultList[0]
	pi.Items = nil                    // 16 要素へのポインタ (rhsKind の IndexExpr)
	rs2 := rep.Results
	rs2[0].Size = 0                   // 17 スライスの別名 (rhsKind の Results 特例)
	rep.Results[0] = r                // 18 要素まるごとの差し替え (Total が腐る)
	r.Items[0] = disk.Item{}          // 19 要素まるごとの差し替え (Size が腐る)
	apply := func(rp *disk.Report) {
		rp.Total = 0                  // 20 関数リテラルの引数経由
	}
	apply(h.diskRep)
}

// production の複合リテラル (テストでは報告しない側。inTest で分ける)
func canaryLiteral(rs []disk.Result, its []disk.Item) (disk.Report, disk.Result) {
	return disk.Report{Results: rs}, disk.Result{Items: its} // 21, 22
}

// 要素の型が省略されたスライスリテラル (外側の型から降ろさないと見えない)
func canaryElidedLiteral(its []disk.Item) []disk.Result {
	return []disk.Result{{Items: its}, {Items: nil}} // 23, 24
}

// 配列 / map / Report のスライスも同じ (4 周目 P2-1 / P3-1。変異が生存したので足した)
func canaryElidedOther(its []disk.Item, rs []disk.Result) ([2]disk.Result, map[string]disk.Result, []disk.Report) {
	return [2]disk.Result{{Items: its}}, // 25 配列
		map[string]disk.Result{"a": {Items: its}}, // 26 map の値
		[]disk.Report{{Results: rs}} // 27 Report のスライス
}

// パッケージレベルの owner への書き込み
func canaryPkgVar(rs []disk.Result) {
	pkgRep.Results = rs // 28
}

// 以下は**報告されてはいけない**形 (要素の差し替えの型ガード / 短い名前)
func canaryNegative2(o otherList, h *holder, c cache, it disk.Item) {
	o.Results[0] = c   // 別の型の Results 要素
	o.Items[0] = ""    // 別の型の Items 要素
	h.rs[0].Items = nil // 短すぎるので owner に採らない (誤検出の芽)
	_ = it
}

// 別の型で再宣言したら前の束縛を持ち越さない (誤検出してはいけない側)
func canaryRebind(rep disk.Report) {
	rep.Total = 0 // 26 (ここまでは owner)
	{
		var rep cache
		rep.Total = 0 // 報告されてはいけない (別の型での再宣言)
		_ = rep
	}
	other := loadCache()
	other.Total = 0 // 報告されてはいけない (型を追えない := は束縛を持ち越さない)
}

func loadCache() cache { return cache{} }

// 関数リテラルの引数が同名で別の型なら、外側の owner の束縛を持ち越さない
// (誤検出してはいけない側。変異 M22 が緑で生存したので足した)
func canaryNegativeArgRebind(rep disk.Report) {
	inner := func(rep cache) { rep.Total = 0 } // 報告されてはいけない
	inner(cache{})
	_ = rep
}

// range の束縛も同じ (変異 M23 が緑で生存したので足した)
func canaryNegativeRangeRebind(rep disk.Report, rows []cache) {
	for _, rep := range rows {
		rep.Total = 0 // 報告されてはいけない
		_ = rep
	}
}

// 以下は**報告されてはいけない**形
func canaryNegative(c cache, eo disk.EntryOutcome, it disk.Item, rep disk.Report) bool {
	c.Total = 0        // 別の型の Total
	c.Items = nil      // 別の型の Items
	eo.Items = nil     // disk.EntryOutcome.Items (Result ではない)
	it.Size = 0        // disk.Item.Size (Result ではない)
	return rep.Total == 0 && len(rep.Results) == 0
}
`

func TestDerivedFieldsGoThroughOwner(t *testing.T) {
	fset := token.NewFileSet()

	// 🚨 canary: 既知の違反と「報告してはいけない形」を**本走査と同じ関数**に通す。
	// これが無いと、判定が壊れて 0 件になっても「違反 0 件 = 緑」で通る。
	canaryFile, err := parser.ParseFile(fset, "zz_canary.go", derivedCanarySrc, 0)
	if err != nil {
		t.Fatalf("canary をパースできない: %v", err)
	}
	canaryFields := ownerFields{}
	canaryConflict := map[string]bool{}
	canaryShort := map[string]bool{}
	diskScanner{diskName: "disk"}.collectOwnerFields(canaryFile, canaryFields, canaryConflict, canaryShort)
	for name, want := range map[string]ownerKind{"Disk": ownerReport, "diskRep": ownerReport, "resultList": ownerResultSlice} {
		if canaryFields[name] != want {
			t.Fatalf("canary のフィールド表が壊れている: %s=%v (want %v)", name, canaryFields[name], want)
		}
	}
	if !canaryConflict["Total"] || !canaryConflict["Items"] {
		t.Fatal("canary: 非 disk 型のフィールド名が衝突として記録されていない (誤検出を止める側が効いていない)")
	}
	// 🚨 短すぎる名前を採らない側が効いているか (採ると `h.r.Size = 0` のような
	// 無関係な型への書き込みを違反と報告する。敵対レビュー 2 周目 P1-1)
	if !canaryShort["rs"] || canaryFields["rs"] != ownerNone {
		t.Fatalf("canary: 短い owner 名 %q を採ってしまっている (誤検出の芽)", "rs")
	}
	canaryHits, canaryResolved := scanDerivedWrites(fset, "zz_canary.go", canaryFile, canaryFields)
	if len(canaryHits) != 29 {
		t.Fatalf("canary の検出が %d 件 (期待 29)。判定が壊れている:\n  %s",
			len(canaryHits), strings.Join(canaryHits, "\n  "))
	}
	// 🚨 同じ canary を `_test.go` として通すと、複合リテラルの 2 件だけが消えるはず。
	// 「production だけに掛ける」という射程が実装と合っていることをここで固定する
	// (合格側の canary。片側だけ見ると、分岐を殺しても気づけない)。
	testHits, _ := scanDerivedWrites(fset, "zz_canary_test.go", canaryFile, canaryFields)
	if len(testHits) != 22 {
		t.Fatalf("_test.go 扱いの canary が %d 件 (期待 22 = 29 - 複合リテラル 7)。"+
			"複合リテラルの射程 (production だけ) が実装と合っていない:\n  %s",
			len(testHits), strings.Join(testHits, "\n  "))
	}
	for _, h := range testHits {
		if strings.Contains(h, "複合リテラル") {
			t.Fatalf("_test.go で複合リテラルを報告した (射程外のはず): %s", h)
		}
	}
	if canaryResolved == 0 {
		t.Fatal("canary: 型を解決できた参照が 0 件 (kindOf が壊れている)")
	}
	for _, h := range canaryHits {
		if strings.Contains(h, "canaryNegative") {
			t.Fatalf("canary: 報告してはいけない形を報告した (誤検出): %s", h)
		}
	}

	// 本走査。所有者パッケージ (disk) 自身は射程外 (ヘッダの脅威モデル参照)。
	root := filepath.Join("..", "..") // src/ (テストの cwd は src/doctor/disk)
	// 🚨 **root からの相対で組むこと**。`filepath.Join("..","disk")` = "../disk" は
	// WalkDir が渡してくる "../../doctor/disk" と一致せず、**この除外は一度も発火しなかった**
	// (敵対レビュー 2026-09-14 P2-4)。今は package disk が自分を import しないので無害だが、
	// `package disk_test` の外部テストを 1 本足した瞬間に、故意の不整合 fixture を
	// 誤検出して赤くなる形だった。
	owner := filepath.Clean(filepath.Join(root, "doctor", "disk"))
	type parsed struct {
		path string
		file *ast.File
	}
	var files []parsed
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case "testdata", "vendor", "tmp", ".git":
				return filepath.SkipDir
			}
			if filepath.Clean(path) == owner {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		f, perr := parser.ParseFile(fset, path, nil, 0)
		if perr != nil {
			return perr
		}
		files = append(files, parsed{path: path, file: f})
		return nil
	})
	if err != nil {
		t.Fatalf("走査できない: %v", err)
	}

	fields := ownerFields{}
	conflict := map[string]bool{}
	short := map[string]bool{}
	consumers := 0
	modules := map[string]bool{} // consumer が居る src/ 直下の module
	for _, p := range files {
		name := diskLocalName(p.file)
		if name == "" {
			continue
		}
		consumers++
		// "../../<module>/<何か>" の形のときだけ module 名として数える
		// (`src/tools.go` のような直下のファイルを module 扱いしない。敵対レビュー 2 周目 P3-2)
		if parts := strings.Split(filepath.ToSlash(p.path), "/"); len(parts) > 3 {
			modules[parts[2]] = true
		}
		// 🚨 **フィールド表は production からだけ集める**。テストの table 構造体の
		// フィールド名 (`sel` / `want` / `got` …) まで表に入れると、テストを 1 つ改名した
		// だけで doctor のこの検査が「衝突で落ちた」と赤くなる (敵対レビュー 3 周目 P2-3 が
		// `sel` → `selected` の改名で実測)。テストが宣言した struct フィールド経由の
		// 書き込みは見逃すが、誤検出とトリップワイヤの面をそのぶん減らす
		if strings.HasSuffix(p.path, "_test.go") {
			continue
		}
		diskScanner{diskName: name}.collectOwnerFields(p.file, fields, conflict, short)
	}
	var lost []string
	for name := range fields {
		if conflict[name] {
			lost = append(lost, name)
		}
	}
	sort.Strings(lost)
	for _, name := range lost {
		delete(fields, name)
	}

	// 🚨 走査が壊れて何も見なくなっても緑にならないよう下限を置く。
	// **owner 表の assert より先に見ること**: 走査が空振りしていると表も空になり、
	// 「衝突で表から落ちた」という誤った診断が出る (変異 M5 で実測した)。
	// 2026-09-14 実測: .go 252 件を走査し (所有者パッケージの 18 件を除いた src/ 全体)、
	// うち doctor/disk を import しているのは 18 件。
	if len(files) < 100 {
		t.Fatalf("走査した .go が %d 件しかない (下限 100)。WalkDir の除外が壊れている", len(files))
	}
	if consumers < 10 {
		t.Fatalf("doctor/disk を import しているファイルが %d 件しかない (下限 10)。"+
			"import の判定が壊れているか、走査が glogx へ届いていない", consumers)
	}

	// 🚨 **走査が届く module が CI の paths filter に居ることを、yml を読んで確かめる**。
	// 走査根は src/ なので、将来 lockman / schedkeys 等が doctor/disk を取り込むと
	// この検査の射程には入るが、paths に居なければ「その module だけを触った push」で
	// 1 度も走らない。allowlist をここへ書くと **yml から paths を消す退行を止められない**
	// ので (敵対レビュー 2 周目 P1-3)、yml 自身を出典にする。
	const workflow = "../../../.github/workflows/src_doctor.yml"
	wf, err := os.ReadFile(workflow)
	if err != nil {
		t.Fatalf("CI の配線を確認できない (%s): %v。移動・改名したなら、この検査の "+
			"workflow 定数とヘッダも直すこと", workflow, err)
	}
	// 🚨 **部分文字列で見ない**。生テキストを `strings.Contains` で照合していた版は、
	// **コメントアウト**した行も、`paths:` を `paths-ignore:` に反転 (意味が真逆) しても、
	// paths を全撤去して説明コメントに文字列だけ残しても、**全部緑**だった
	// (敵対レビュー 3 周目 P1-1 が 5 形で実測)。行を構造として読む。
	triggers, perr := workflowPaths(string(wf))
	if perr != nil {
		// 🚨 「paths が消えた」と「**構造として読めなかった**」を別のメッセージにする。
		// 混ぜると、yamlfmt をかけただけ / フロー形式へ DRY 化しただけの人が
		// 「paths を消したと言われるが在る」で 1 往復する (4 周目 P2-2)
		t.Fatalf("%s を**構造として読めなかった**: %v\n"+
			"paths を消したという意味ではない。yml の書き方 (フロー形式 / アンカー / "+
			"インデント幅 / タブ) を変えたなら workflowPaths も直すこと", workflow, perr)
	}
	for _, name := range []string{"push", "pull_request"} {
		if len(triggers[name]) == 0 {
			t.Fatalf("%s の %s に paths が 1 件も無い。paths を消した / paths-ignore へ変えたなら、"+
				"この検査が守っている前提 (glogx の変更で doctor の CI が走る) が崩れている",
				workflow, name)
		}
		// canary: 自分の module は必ず居るはず (居なければ抽出が壊れている)
		if !slices.Contains(triggers[name], "src/doctor/**") {
			t.Fatalf("%s の %s の paths から 'src/doctor/**' を抽出できない (抽出が壊れている): %v",
				workflow, name, triggers[name])
		}
	}
	for m := range modules {
		for _, name := range []string{"push", "pull_request"} {
			if !slices.Contains(triggers[name], "src/"+m+"/**") {
				t.Fatalf("doctor/disk の consumer が src/%s に居るのに、%s の %s の paths に "+
					"'src/%s/**' が無い。src/%s だけを触った push ではこの検査が 1 度も走らない。"+
					"抽出した paths: %v", m, workflow, name, m, m, triggers[name])
			}
		}
	}

	// 🚨 **衝突で表から落ちた owner を明示する**。名前が別の型でも宣言されていると
	// 誤検出を避けるために表から落とすが、落ちた名前を経由した書き込みは検出できなくなる。
	// 黙って弱くならないよう、落ちた名前を既知の一覧と突き合わせる (増えたら落ちる)。
	//
	// 既知の盲点 `rep`: `doctorDiskEvent.rep *disk.Report` (glogx doctor_view.go。走査 goroutine から
	// model へ Report を運ぶ production のフィールド) が、`doctorSvcMsg.rep svc.Report` と
	// `doctorDeleteEvent.rep *disk.DeleteReport` と名前衝突して落ちている。つまり
	// `msg.ev.rep.Results = …` はこの検査を素通りする (敵対レビュー 2026-09-14 P1-4 が実測)。
	// 型解決を go/types に持ち込まない限り塞げないので、**盲点として宣言**する。
	if len(lost) > 0 {
		t.Fatalf("owner フィールド名 %v が別の型との衝突で表から落ちた (この名前を経由した "+
			"書き込みは検出できなくなる)。意図した劣化なら、ここへ理由つきの許可リストを "+
			"足したうえでヘッダの盲点一覧にも書くこと", lost)
	}

	// 🚨 **短くて採らなかった owner 名 = 宣言された盲点**。増減したら落ちる (双方向)。
	// これらを経由した書き込み (`msg.ev.rep.Results = …` / `ev.r.Items = …`) は素通りする。
	// 採ると誤検出になる (ownerFieldMinLen のコメント参照) ので、盲点として引き受ける。
	//   rep: doctorDiskEvent.rep *disk.Report  (走査 goroutine から model へ運ぶ production の口)
	//   r  : doctorDiskEvent.r   *disk.Result  (同上)
	knownShort := []string{"r", "rep"}
	gotShort := make([]string, 0, len(short))
	for n := range short {
		gotShort = append(gotShort, n)
	}
	sort.Strings(gotShort)
	if !slices.Equal(gotShort, knownShort) {
		t.Fatalf("短くて owner に採らなかったフィールド名が変わった: %v (既知 %v)。"+
			"増えたなら盲点が増えている / 減ったなら盲点が解消した。どちらもヘッダの記述と "+
			"knownShort を直すこと", gotShort, knownShort)
	}

	// 🚨 実在する owner が表に居ることを固定する。フィールド名が衝突すると表から落ちて
	// **検査が黙って弱くなる**ので、そのときはここで落ちて気づける (issue 372)。
	for name, want := range map[string]ownerKind{"Disk": ownerReport, "diskRep": ownerReport, "diskResults": ownerResultSlice} {
		if fields[name] != want {
			names := make([]string, 0, len(fields))
			for n := range fields {
				names = append(names, n)
			}
			sort.Strings(names)
			t.Fatalf("owner フィールド表に %s (%v) が居ない。同名フィールドが別の型でも宣言されて "+
				"衝突で落ちた可能性がある (落ちると検査が黙って弱くなる)。表: %v", name, want, names)
		}
	}

	var offenders []string
	resolved, withoutFields := 0, 0
	for _, p := range files {
		o, r := scanDerivedWrites(fset, p.path, p.file, fields)
		offenders = append(offenders, o...)
		resolved += r
		// 空の表でもう一度走らせて「表が判定に効いているか」を測る (下の assert 用)
		_, r0 := scanDerivedWrites(fset, p.path, p.file, ownerFields{})
		withoutFields += r0
	}

	// 🚨 型解決そのものが生きていることの下限 (2026-09-14 実測: 161 件)。
	// 違反を直し切ると違反数は 0 になるので、**判定が生きている証拠**はここで稼ぐ。
	if resolved < 60 {
		t.Fatalf("型を解決できた Report/Result への参照が %d 件しかない (下限 60)。"+
			"kindOf が壊れている", resolved)
	}
	// 🚨 フィールド表が**本走査に配線されている**ことを固定する。表を作っても渡し忘れれば
	// `sn.Disk.Results` / `v.diskRep.Results` の経路が丸ごと見えなくなるが、canary は
	// 自前の表を作るので緑のまま通ってしまう (変異 M6 で実測した)。
	// 閾値ではなく「表あり > 表なし」で見るので、実測値が動いても腐らない。
	if resolved <= withoutFields {
		t.Fatalf("フィールド表が判定に効いていない (表あり %d 件 = 表なし %d 件)。"+
			"本走査へ fields を渡していない可能性がある", resolved, withoutFields)
	}
	if len(offenders) > 0 {
		t.Errorf("disk.Report / disk.Result の導出フィールドを所有者の外から直接書いている "+
			"(Results は WithResults、Items は WithItems を通すこと。通さないと Total / Size が "+
			"古いまま残る。issue 372):\n  %s", strings.Join(offenders, "\n  "))
	}
	t.Logf("走査 .go=%d 件 / disk を import=%d 件 / 型解決できた参照=%d 件 / 違反=%d 件",
		len(files), consumers, resolved, len(offenders))
}
