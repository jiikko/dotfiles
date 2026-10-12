# 716 (refactor): treefiler・glogx・pro-con で似た部品を別々に持っている (調べた結果、ほぼ寄せない)

起票日: 2026-10-12

> **完了 (2026-10-12)**: 反証レビューの結果、候補の大半は「同じ中身」ではなかった (下表の「寄せない」)。「寄せてよい小さなもの」の 3 つは寄せた (進捗)。
> 「寄せない」を見直すのは、3 つ目の利用者が現れたとき。

## 概要

dotfiles-5f の調査 (2026-10-12、読むだけ) で、2 つ以上の module が似た処理を別々に持っている箇所を挙げ、反証レビュー (dotfiles-5f、sonnet)
で中身を突き合わせた。共通の部品の置き場は `src/tuikit`。寄せるのは「利用者が 2 つ以上実在し、中身が同じ」ものだけ
(`~/.claude/rules/verify-design-intent-before-refactor.md`)。

## 寄せない (理由つき。再提起するなら理由を崩す実測・事実を添える)

| 候補 | 寄せない理由 |
|---|---|
| treefiler `filer/tile.go` `tileKey` のスクロールを `listnav.Scroll` に | 同じなのは増減の量だけ。tileKey は「増減 → `t.src.ensure` で読み足す → `maxScroll` で clamp」の順で、Scroll は渡された total で先に clamp する。total は遅延読み込みで増えるので、置き換えると「まだ読んでいない続きへ下りられない」か「読む量の基準が変わる」。Down / HalfDown / Bottom のどれも同じ |
| 入力欄の「頭 + `lineedit.Window` + キャレットの桁」(glogx `promptWindow`・treefiler `inputField`・pro-con `ui/search.go` / `ui/view.go` / `ui/answerform.go`) | 幅から引く量 (`w-頭` / `w-頭-1` / 下限の有無 / `w-1`) とキャレットの桁の契約 (pro-con は頭の幅を足さない) が違い、重複は 2 行ほど。寄せると呼び出し側の契約が変わる。treefiler の 1 桁は issue 715 で直す |
| スクロールバーのつまみ (tuikit `layout/scrollbar.go` `Scrollbar` と treefiler `drawTile`) | 出力 (文字列の行 / canvas のセル)・字形・色・丸め (四捨五入 / 切り捨て)・長さの上限・tween の描画位置で動くかが違う。共通は位置の算術 1 行。寄せると見た目が 1 行ずれる |
| `filer.ExecRequest` → `*exec.Cmd` (treefiler `main.go` と glogx `external_commands.go` `filerExecCommand`) | treefiler は `exec_boundary_test.go` で非テストの `os/exec` を禁じて `subproc.CommandContext` を使い、glogx は `exec.Command` に「subproc: no-waitdelay」の免除を付けている。規約の書き方が逆なだけで結果は同じ (前景・ctx 無し・パイプ無し)。組み立ては 3 行で、寄せ先を filer に置くと glogx に subproc を強いる |
| スピナーの字形の表 (glogx `render.go`・treefiler `model.go`・restartable `internal/ui/view.go`) | コマの選び方が違う (tick / 80ms の壁時計)。restartable は tuikit を擬似バージョンで固定しているので、届く利用者は今は 2 つ |
| 本文 pager の流用 | issue 666 (pending) の D。trigger (3 つ目の pager) が引かれていない |
| filer の tween (`filer/anim.go`) と `tuikit/anim` | 中身が違う (dt で進み途中から始め直す / 壁時計) |
| 最下行の hint の落とし方 (treefiler `statusBar` と glogx `fitHintItems`) | 方式が違う (優先度で採る / 区画ごとに固定の順。issue 279)。寄せると見た目が変わる |
| git porcelain の解析 (treefiler `git.go` `parsePorcelain` と glogx `worktree_status.go` `parseWorktreeStatus`) | 出すものとブランチの表記が違い、tuikit の責務外 |
| リンクのジャンプ (treefiler `tile.go` と glogx `issues/filelink.go`) | 解決の規則と見せ方が違い、共通なのは剰余算だけ |
| 枠 (filer `canvas.frame` と `tuikit/layout.Panel`)・RGB の補間 (filer `palette.go` `mix` と pro-con `mixRGB`) | 描く仕組み・型が違い、寄せても良くならない |

## 寄せてよい小さなもの (2026-10-12 に寄せた)

- 16 色の値の表: treefiler `render.go` の ansi16 と pro-con `switchfade.go` の basic16 は値が一致する (型は float64 / int、範囲外の扱いが違う)。
  寄せ先の候補は `tuikit/sgr` だが、README の責務 (基本の ANSI 色・装飾) の拡張になるので置き場から決める
- treefiler `tile.go` `jumpKey` の末尾 4 行は `listnav.WindowOffset` と同じ中身 (置き換えるだけ)
- 巡回する選択の剰余算 `(i±1)%n` (glogx `url_picker` / `linkJumpKey`、treefiler `jumpKey` / `panelKey` / `stepMatch` など): 件数は数え方で
  12〜16 と揺れる (別の意味の剰余を含むかで変わる)。寄せるなら算術だけ

## 関連

- issue 715 (filer の入力の層。入力欄の `inputField` の 1 桁)
- issue 666 (pending。本文 pager の D)

## 進捗

- 2026-10-12: 起票
- 2026-10-12: 反証レビュー (dotfiles-5f、sonnet) を当てた。tileKey のスクロール (遅延読み込みとの順序)・入力欄 (キャレットの契約)・
  スクロールバー (丸めと出力)・ExecRequest (規約の向き) を「寄せない」へ移し、結論を「ほぼ寄せない」に書き直して pending へ。
  反証できなかった主張: tuikit の参照の仕方 (restartable 以外は replace)・入力欄の組み立てが 3 module 6 か所に実在・丸めの違い・666 の D の扱い
- 2026-10-12: ユーザーの指示で「寄せてよい小さなもの」の 3 つを寄せた (commit「refactor(tuikit): 256 色の RGB と巡回の剰余算を tuikit に寄せる」)
  - 16 色の表: 置き場は `tuikit/sgr` に決め、256 色の番号全体を RGB にする `sgr.XtermRGB` にした (treefiler の ansi16 / xterm256 と、
    pro-con の basic16 / cubeLevels / rgb256 は同じ表の 2 実装だった)。sgr の doc と README の責務に「256 色の番号が表す RGB」を足した
  - treefiler `jumpKey` の窓の追従を `listnav.WindowOffset` へ。旧来の式との違いは上限の clamp (`len(view)-rows`) が加わることだけ
  - 巡回の剰余算: `listnav.Cycle(i, delta, n)` を足し、glogx 7・treefiler 9・pro-con 2 の計 18 か所を置き換えた
    (`grep -c 'listnav.Cycle('` で数えた)。treefiler `virt.go` の `advClock` の剰余は輪のバッファで、選択の巡回ではないので残した
  - 旧式との一致: 置き換えた式を i=-1..n-1 (-1 は indexOfStr の見つからない)・d=±1・n=1..50 で総当たりして食い違い 0。
    256 色は 16〜255 で旧 2 実装と一致、16 色の表は旧 2 つのリテラルと同じ値 (pro-con の旧 rgb256 は 16 未満で配列の範囲外へ出たが、
    呼び出し元は 16 以上しか渡していなかった)
  - 変異 (mutate-verify で red): Cycle を `(i+delta)%n` / `(i+delta+n)%n` に戻す・灰の階調を 1 段ずらす・基本 16 色の 1 つを変える・範囲の外の上限を外す。
    `(i+delta+n)%n` は最初の版の TestCycle では緑のまま通ったので、旧来の式なら負になる組に差し替えた
  - make test / lint: tuikit・treefiler・pro-con・glogx の 4 module で rc=0。glogx は負荷の高い回 (load 7〜16) に doctor の待ちのテスト 2 本
    (`TestRescanCancelsPreviousGeneration` / `TestCursorFallbackIsToldThroughBrowseModel`) が 2 回時間切れになった。単独でも、3 回目の全体でも緑で、
    今回の変更の経路 (listnav / sgr) を通らない
  - 敵対的レビューは省略した: 判断のロジックを足さない置き換えで、一致を総当たりで確かめたため

