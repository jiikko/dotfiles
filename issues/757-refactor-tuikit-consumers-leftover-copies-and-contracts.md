# 757 (refactor): tuikit に寄せた後の、使う側に残った写しと暗黙の約束

起票日: 2026-10-14

## 概要

2026-10-14 の tuikit の監査 (記録は 760) の設計の観点。どれも今は誤動作しない (潜在)。前回 (700・716) に寄せた範囲の外に残ったもの。

## 詳細

1. **`foreground.Exec` の結果 (取り戻せたか) が公開フィールドの副作用でしか受け取れず、doc と lint が勧める書き方では黙って消える** (leaky-abstraction)
   - `src/tuikit/foreground/exec.go` の `NewExec` の doc と、`glogx/gorules/rules.go`・`treefiler/gorules/rules.go` の `execViaForeground` の Report は
     「`tea.Exec(foreground.NewExec(cmd), fn)` と使う」と案内する。この形では `Exec.Was` / `Exec.Err` を読めない
   - 結果を拾っている 3 つ (`glogx/tui.go`・`treefiler/main.go`・`pro-con/ui/terminal.go` の `termReturn`) は、`x := NewExec(cmd)` を作ってから
     クロージャで `x.Err` を読む形を手で書いている
   - 発火条件: 4 つ目の使う側が doc / lint どおりに書くと、前面を取り戻せなかった理由が画面に出ない
   - 直し方: doc と lint の文言を `x` を作ってから読む形に直す。または結果を受け取る形を API に足す
2. **`tea.ExecProcess` を止める lint が pro-con に無く、glogx と treefiler に同じ規則が 2 つある** (duplication)
   - `execViaForeground` を持つのは `glogx/gorules` と `treefiler/gorules` だけ。pro-con には gorules が無く、`.golangci.yml` にも同じ規則が無い
   - 今は pro-con の起動口が `termExec` 1 本に寄っているので壊れていない。issue 707 の進捗に pro-con を外した理由の記述は無いが、
     `pro-con/.golangci.yml` の冒頭に「gocritic の ruleguard: 構文の不変条件の規則をまだ持たない」とある。足すには ruleguard の仕組みごと新設するコストがかかる
   - 拾い残し: pro-con のコメントに `tea.ExecProcess` が 3 か所残っている (`main.go`・`backend/backend.go`・`fake/fake.go`。反証レビュー)
   - ついで: `glogx/waitdelay_discipline_test.go` の allowlist の説明が「(tea.ExecProcess)」のまま (実体は `runEditorCmd`。707 の拾い残し)
3. **巡回の剰余算と移動キーの語彙の手書きが残っている** (duplication)
   - 剰余算: `pro-con/ui/legend.go` (タブの巡回。listnav は既に import 済み)・`schedkeys/form.go` 3 か所・`schedkeys/model.go` 2 か所 (main が grep で確認)。
     issue 716 は置き換えた数だけを数えていて、残りを数えていない。剰余の意味が別のもの (リングバッファ・割り算・検索の走査) は除いた
   - 移動キー: `schedkeys/model.go` の `keyMenu` と `keyPick` が j/k/↓/↑/ctrl+n/ctrl+p を手で並べ、`keyPick` では g/G/home/end/ctrl+d が効かない。
     issue 700 の 3 は「listnav を使うファイル」だけを数えていた
   - 置き換えて答えが変わる組は見つからなかった (監査体が `listnav.Cycle` と突き合わせ)
   - 直し方: legend と schedkeys を `listnav.Cycle` / `MotionOf` に寄せる (schedkeys に寄せるかは UI ガイドの適用範囲次第)
   - 🚨 `MotionOf` へ寄せると意味が変わりうる: `keyMenu` は tab / shift+tab も移動に使って末尾で先頭へ巡回し、`keyPick` は端で止まる。「答えが変わらない」と確かめたのは Cycle だけ
4. **glogx の `srcGutter` が `gutter.Format` の出力の末尾を「1 バイトの空白」と決め打ちして切っている** (leaky-abstraction)
   - `glogx/issues_view.go` の `srcGutter` の `s[:len(s)-1]`。treefiler (`filer/tile.go`) は溝全体を 1 色で塗るので、この前提を持たない
   - 発火条件: 区切りを `│` のような複数バイトの文字に変えると、glogx 側で文字の途中で切れる
   - 反証レビューの訂正: 「末尾に空白 1 つ」は `gutter.Format` の doc に書かれた契約で、`TestFormat` が `"  7 "` で固定している。区切りを変えれば gutter 自身のテストが先に落ちるので、
     暗黙の前提ではなく文書化された契約への依存 (優先度は低い)
   - 直し方: gutter が番号の部分と区切りを分けて返す (例: `Number(src, width)`) か、色付けの関数を受け取る
5. **端末のエスケープ列を手で読む走査が使う側に 4 つあり、終わりの見方が 3 通り** (duplication)
   - `pro-con/ui/search.go` の `markHits` (ESC の後ろを「次の `m` まで」。700 の 1 で termwidth から消した読み方)・`pro-con/ui/switchfade.go` の `dimANSI` と
     `treefiler/filer/render.go` の `putSGR` (CSI の終端バイト 0x40〜0x7e まで)・`glogx/render.go` の `reapplyAfterReset` (固定の並びとの一致)
   - どの走査にも SGR 以外のエスケープ (OSC 8 など) が届く経路は見つからなかった。`markHits` に渡るカードのタイトルが取り込みで無害化されているかは未確認
   - 直し方: termwidth の `escLen` (x/ansi の `DecodeSequence`) を公開し、少なくとも `markHits` を置き換える

## 却下 (再提出しない)

- 前面の取り戻しを使う側が手で組むのを禁止する案 (3 つとも NewExec を通っている)・`listnav.Cycle` の n=0 (18 か所すべて空を先に弾く)
- pro-con と treefiler の SGR の解釈と RGB の補間を寄せる案 (716 で却下済み)・`doctor_delete` の移動キーと restartable の confirm の別実装 (コメントに理由)
- schedkeys の toast (CLAUDE.md に理由)・`StripSGR` の名前 (700 で決めた)・`srcGutterWidth` の別名・markdown の 2 つの戻り値の長さの確認

## 関連ファイル

- `src/tuikit/foreground/exec.go`・`src/tuikit/gutter/`・`src/tuikit/termwidth/`・`src/{glogx,treefiler}/gorules/rules.go`・`src/pro-con/ui/`・`src/schedkeys/`
- 前回: 700・716・707。監査の記録: 760

## 進捗

- [ ] 1〜5 は未着手
