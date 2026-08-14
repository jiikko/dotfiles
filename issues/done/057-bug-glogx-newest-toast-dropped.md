# glogx: 狭い端末で「最新のトースト」が 1 行も描かれなくなった (doc の不変条件とコードが乖離)

起票日: 2026-08-14
種別: bug
優先度: **P2** (ユーザーが今押したキーのフィードバックが画面に出ない。表示のみでデータは壊れない)

> ⚠️ 起票時は P1 としたが、敵対的検証で「常に永久消失」ではないと判明したため P2 へ訂正した。
> **永久に失われるのは「抑制中 (~3.2s) に次のトーストが来る」「警告が出続ける」場合のみ**で、
> 警告 1 枚だけなら 3.2 秒遅れて描かれる (下表)。操作フィードバックとしてはどちらも死んでいるが、
> データ損失を伴う issue 058 と同じ段には置かない。

## 何が起きるか

端末が狭いとき、**直前に出したトーストが 1 枚も描かれず、古い警告だけが残る**。
ユーザーから見ると「キーを押したのに何も起きていない」ように見える。

発火するのは「最新が成功 (`✓`) か進行中 (`…`) で、残っているのが警告 (`✗`)」のとき。

⚠️ **「永久に出ない」とは限らない (敵対的検証で範囲を訂正)**。実時間 (33ms/frame,
`toastHold=3s`) で測ると 2 通りに分かれる:

| 状況 | 結果 |
|---|---|
| 警告が 1 枚だけで、以後トーストが来ない | 古い警告が抜け切る **~3.2 秒後にようやく描かれる** (実測: 押下 frame 15 → 描画 frame 113 = **3234ms 遅延**。`4f36991^` は 33ms) |
| 抑制中 (~3.2s) に次のトーストが来る / 警告が出続ける | **最後まで 1 度も描かれない** (実測 `ever=false`) |

どちらも操作フィードバックとしては死んでいる (3.2 秒後の「コピーしました」は、
ユーザーが既に次の操作へ移った後に出る)。

実操作の例 (高さ 25 の tmux popup):

1. `git push` に失敗して警告トーストが出ている
2. `y` (URL コピー) を押す → **「コピーしました」が一切表示されない**。古い警告 2 枚だけが残る
3. `p` (PR を開く) の「PR を検索中...」も同様に一切出ない

`boxLines` の doc は真逆を明記している:

> 最新の 1 枚は上限を超えても出す — 見えない通知より「窓を覆うが読める通知」を選ぶ

**doc はそのまま残り、コードだけが変わっている。**

## 再現 (A/B 実測)

`src/glogx` に置いて `go test -run TestRVNewestToastDropped .`:

```go
package main

import (
	"strings"
	"testing"
)

func TestRVNewestToastDropped(t *testing.T) {
	cases := []struct {
		name     string
		maxLines int
		build    func(*toast)
		want     string
	}{
		{"fit=1 (端末高さ20)", 7, func(s *toast) {
			s.show("git push に失敗しました", false)
			s.show("コピーしました", true)
		}, "コピーしました"},
		{"fit=2 (端末高さ25)", 10, func(s *toast) {
			s.show("警告A", false)
			s.show("警告B", false)
			s.show("コピーしました", true)
		}, "コピーしました"},
		{"info (進行中)", 10, func(s *toast) {
			s.show("警告A", false)
			s.show("警告B", false)
			s.showInfo("PR を検索中...")
		}, "PR を検索中"},
	}
	for _, c := range cases {
		var s toast
		c.build(&s)
		for range toastSlideFrames + 2 {
			s.advance(false)
		}
		got := strings.Join(s.boxLines(false, c.maxLines), "\n")
		if !strings.Contains(got, c.want) {
			t.Errorf("%s: 最新のトースト %q が 1 行も描かれない:\n%s", c.name, c.want, got)
		}
	}
}
```

`toast.go` だけを差し替えて同じテストを回した結果 (worktree b49b30b):

| toast.go の版 | 結果 |
|---|---|
| `997d078` (昨晩の変更前) | **ok** |
| `7fdb150` | ok |
| `6a684e2` (トーストの追い出しを重要度つきに) | ok |
| **`4f36991`** (保持と表示で落とす順の規則を共有する) | **FAIL** |
| `b49b30b` (HEAD) | **FAIL** |

→ **導入コミットは `4f36991`**。

HEAD での実出力 (fit=2 のケース。「コピーしました」が消えている):

```
┌─────────┐
│ ✗ 警告B │░
▖▁▁▁▁▁▁▁▁▁▗▒
  ░▒▒▒▒▒▒▒▒▒
┌─────────┐
│ ✗ 警告A │░
▖▁▁▁▁▁▁▁▁▁▗▒
  ░▒▒▒▒▒▒▒▒▒
```

## 原因

`toast.go` の `boxLines`:

```go
fit := max(maxLines/toastBoxLines, 1) // 最新の 1 枚は上限を超えても出す
for i := len(boxes) - 1; i >= 0 && len(boxes) > fit; i-- {
    if !shown[i].important() {
        boxes = append(boxes[:i], boxes[i+1:]...)
        shown = append(shown[:i], shown[i+1:]...)
    }
}
```

`items()` は**新しい順**に返す (index 0 = 最新)。ループは末尾 (最古) から `i=0` まで走り、
`important()` でない枚を落とす。**`i` が 0 まで到達するので、最新が成功/進行中なら最新が落ちる。**

`max(..., 1)` が保証するのは `fit >= 1` (=「1 枚は出す」) だけで、
**その 1 枚が最新であることは保証していない**。コメントはそのつもりで書かれている。

### なぜ「共有」した commit で壊れたのか

`4f36991` は「保持 (`evictOne`) と表示 (`boxLines`) で落とす順の規則を共有する」変更だが、
**共有されたのは `important()` (重要度の判定) だけで、「最新は保護される」という半分が
共有されていない**。

| | 対象 | 最新を落とせるか |
|---|---|---|
| `evictOne` (保持) | `s.older` のみ | **落とせない** (最新は `s.toastItem` に埋め込みで別管理) |
| `boxLines` (表示) | `s.items()` (= 埋め込みの最新を含む) | **落とせてしまう** |

`evictOne` は構造的に最新を触れないので、同じ判定関数を共有しても安全側に倒れる。
`boxLines` は最新を含む列を回すため、同じ判定でも意味が変わる。

なお `4f36991` / `6a684e2` が直したのは「**古い警告**が描かれない」問題 (実測 2026-08-13)。
その修正は正しく効いているが、**鏡像のバグ (最新の成功/進行中が描かれない) を作った**。

## 発火する端末の高さ

`tui.go:2806` / `2894` が `m.toast.boxLines(m.colored, max(page/2, toastBoxLines))` を呼ぶ。
`pageSize()` は framed で `max(height-frameVOverhead, 1)` (`frameVOverhead = 5`)。

`fit = max(maxLines/4, 1)`, `maxLines = max(page/2, 4)` なので framed では:

| 端末の高さ | page | maxLines | fit | 発火 |
|---|---|---|---|---|
| ≤ 20 | ≤ 15 | ≤ 7 | 1 | **する** (警告 1 枚 + 最新で発火) |
| 21〜28 | 16〜23 | 8〜11 | 2 | **する** (警告 2 枚 + 最新で発火) |
| ≥ 29 | ≥ 24 | ≥ 12 | 3 | しない (`toastStackMax = 3` に届かない) |

**非 framed でも発火する** (フレームは幅 <60 / 高さ <15 で自動 OFF、`--no-frame` でも OFF)。
`page = height-1` なので **高さ ≤16 で fit=1**、17〜24 で fit=2。
e2e 実測で `unframed h=16` (`frameActive=false pageSize=15 maxLines=7`) の再現を確認済み。

実描画経路 (`browseModel.View`) での実測 — HEAD で 3 ケースとも FAIL / `4f36991^` で 3 ケースとも PASS:

```
framed   h=20 (fit=1)  frameActive=true   pageSize=15  maxLines=7
framed   h=25 (fit=2)  frameActive=true   pageSize=20  maxLines=10
unframed h=16 (fit=1)  frameActive=false  pageSize=15  maxLines=7
```

framed h=20 の実画面 (「コピーしました」が 1 行も無い):

```
 ║       subject                         ┌────────────────────────────────┐  ║▒
 ║                                       │ ✗ push に失敗: fatal: rejected │░ ║▒
 ║   ⠋ commit ccccccccccccccccccccccccccc▖▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▗▒ ║▒
```

## 既存テストがこの穴を素通りする (false green)

- `TestToastBoxLinesRespectsMaxLines` の `{1, 1} // 上限より箱が大きくても最新 1 枚は出す`
- `4f36991` が追加した `TestToastBoxLinesKeepsWarningWithinBudget` 末尾の
  `if got := len(s.boxLines(false, toastBoxLines)); got != toastBoxLines`

どちらも**行数だけ**を assert する。最新が落ちて古い警告が残っても行数は変わらないので通る。

⚠️ `4f36991` の commit message は「既存の `TestToastBoxLinesRespectsMaxLines`
(最新 1 枚は上限を超えても出す) も無改変で green」を不変条件維持の根拠に挙げているが、
**そのテストはその不変条件を検査していない**。名前だけが不変条件を主張している。

⚠️⚠️ さらに悪いことに、**`4f36991` が追加した `TestToastBoxLinesKeepsWarningWithinBudget` の
末尾 (予算 1 枚) の fixture 自体が、このバグを既に踏んでいる**。同 fixture の出力を中身で見ると
描かれているのは「✗ 警告: 未 push があります」だけで**最新の ok2 は消えている**のに、
行数が 4 のままなので assert は通る。**バグを再現している fixture が green を返していた。**

## 対応方針 (案)

1. **ループを `i >= 1` にして最新 (index 0) を保護する**。doc の「最新の 1 枚は上限を超えても
   出す」と一致させる。最終段の `boxes = boxes[:fit]` は先頭から切るので最新は残る
2. **テストを「行数」でなく「中身」で固定する**。`strings.Contains(出力, 最新のテキスト)` を
   assert する形にしないと、同じ穴がまた通る
3. `important()` の doc に「**この判定は最新を保護しない。最新の保護は呼び出し側の責務**」を
   足す (`evictOne` と `boxLines` で意味が変わる理由がコードから読み取れないため)

## 未確認

- `dropInfo()` 経由で進行中トーストが消える経路と本件の相互作用 (結果トーストが出る前に
  info が落ちるケース) は未検証
- 実機の tmux popup での目視確認は未実施 (ロジックの A/B のみ)

## 関連

- issue 028 P2 / issue 045 — `6a684e2` / `4f36991` の元になった要求
- `_claude/rules/mutation-verify-new-tests.md` — 「green は『その書き方では壊せなかった』」。
  本件は**テスト名が不変条件を主張しているのに検査していない**典型
- `_claude/rules/comment-no-restate-enforced.md` の裏側 — doc に書いた不変条件が
  実装で強制されておらず、コードだけが先に変わった

## 対応記録 (2026-08-15)

方針 1〜3 をそのまま実装:

1. `boxLines` の重要度選別ループを `i >= 1` にして最新 (index 0) を保護。最終段の
   先頭切り (`boxes[:fit]`) は新しい順の先頭を残すので、これで doc の「最新の 1 枚は
   上限を超えても出す」が実装で成立する。2026-08-13 に直した「古い警告が描かれない」側は
   fit=2 のケース (警告 + 最新) で引き続き成立 (既存 assert が守っている)
2. テストを中身で固定: `TestToastBoxLinesKeepsWarningWithinBudget` の予算 1 枚の assert に
   「最新 ok2 が含まれる」を追加 (バグを再現していた fixture が green を返していた箇所)。
   issue の再現 3 ケースを `TestToastBoxLinesAlwaysDrawsNewest` として常設
3. `important()` の doc に「この判定は最新を保護しない。保護は呼び出し側の責務
   (evictOne は対象が構造的に最新を含まないので不要)」を追記

変異検証: ループを `i >= 0` に戻すと新旧 4 assert がすべて red
(「コピーしました」「PR を検索中」「ok2」が 1 行も描かれない) になることを実測して revert。

未確認のうち dropInfo との相互作用は本件の修正で挙動が変わらない (dropInfo は保持側の操作で、
表示選別より前に列から消える) ため追加検証しない。実機 tmux popup の目視も、ロジックが
ユニットテストで機械的に固定できたため実施していない。
