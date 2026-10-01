# 591 (bug): tuikit の highlight が特定の 1 行で数百 ms 止まり、markdown・diff の描画が数秒固まる

起票日: 2026-10-01

## 概要

`highlight.Code` (chroma の lexer で 1 行を色付けする) が、**閉じない文字列リテラル + 長いバックスラッシュの列**を含む行で
lexer の正規表現が爆発的にバックトラックし、1 行あたり約 0.4 秒かかる (chroma の打ち切りで止まり、`Tokenise` の error を
`Code` が握って素の行を返す)。見た目は正しいまま、描画だけが止まる。

`markdown.Render` (色あり) のコードブロックと `highlight.Diff` の各行がここを通るので、そうした行が数行あるだけで画面が数秒固まる。

## 詳細

### 実測 (2026-10-01、tuikit の go.mod の chroma の版、`go run` の probe)

| 入力 | 時間 |
|---|---|
| `highlight.Lang("go", "x := \"" + "\\"×5)` | 0.8 ms |
| 同 ×25 | 25 ms |
| 同 ×60 | 379 ms |
| `markdown.Render` (```go のフェンスに上の ×60 の行を 10 行、幅 100、色あり) | **4.0 秒** |

監査の調査役の測定では、go / js / json / yaml / toml の lexer で同じ形 (N=30 で 264 ms、N≥60 で約 400 ms で頭打ち)、
`colored=false` の Render は 20 µs、`highlight.Diff` に同じ追加行を 10 本渡しても 4 秒。`make` lexer は行の長さに対して
二次 (`a`×8000 の 1 行で 2.5 秒) だった。この 3 点は main では再測していない。

`markdown/markdown.go` の `renderCode` は highlight (`markdown/render.go` の `paintLine`) の前に行を幅で切るが、×30 程度なら幅 80 に収まるので防げない。
`highlight.Diff` の本文の行には長さの上限が無い。

### 呼び出し元と、止まる場所

`grep -rn 'highlight\.\(Lang\|Diff\|Code\)(\|markdown\.Render(' src | grep -v _test` で tuikit の外に 5 か所:

- `src/pro-con/ui/activity.go` の `addActivity`: PG の応答文を `markdown.Render(…, true)` する。**キャッシュが無く**、
  `drawer.go` の `drawerPanel` → `drawerBody` → `addActivity` の経路で引き出しを描くたびに全応答を整形し直す。
  上の行を含む応答が 1 件あると、引き出しを開いている間は**毎フレーム**数秒かかる (経路はコードを読んだ結果。実機での固まり方は未実測)。
  活動を受け取ったとき (`activity.go` の活動の更新) は `drawerBody()` を 1 回の更新で最大 3 回呼ぶので、そのときはさらに重い
- `src/glogx/issues/body.go` の `Body.Lines`: issue 本文。幅・色が同じならキャッシュを返すので、払うのは開いたとき・幅を変えたとき
- `src/glogx/gitlog.go` / `src/glogx/worktree_status.go`: `highlight.Diff` (diff の詳細)
- `src/pro-con/ui/diffview.go`: `highlight.Diff` を `diffLoadedMsg` を作る tea.Cmd の中で呼ぶ (UI は止まらず、表示が遅れる)

### 発火条件

- issue 本文・PG の応答・diff の中のコード行に、閉じない `"` の後ろにバックスラッシュが 30 個以上並ぶ行がある
- かつ、その行に chroma の lexer が付く: markdown ならフェンスの言語名 (```go 等)、diff ならファイルのパス
  (反証レビューの実測: 同じ追加行 10 本の `highlight.Diff` は path `x.go` で 4.04 秒、`x.txt` / `x.md` では 2 ms 前後)
  (正規表現・エスケープのテスト fixture、壊れた JSON、Windows パスの途中で切れた文字列など)
- silent: 出力は素の行で正しいので、テストも見た目も通る。遅さだけが出る

## 対応方針

- `highlight.Code` の入口で、chroma に渡さず素通しに倒す条件を足す。候補: 行の長さの上限 (1〜2 KB)、連続する `\` の上限。
  条件は実測で決め、閾値の根拠 (どの長さで何 ms) をコードに残す
- chroma の正規表現の打ち切り時間 (今は 1 行あたり約 400 ms で効いている) を短くできるかを調べる。lexer ごとの設定か、
  `Tokenise` を予算付きで呼ぶ形にできるか
- pro-con の `addActivity` が毎フレーム `markdown.Render` し直す点は、この件とは別に描画の負荷になる。応答文ごとに
  幅・色をキーにしたキャッシュを持たせる (glogx の `Body.Lines` と同じ形) かを判断する。pro-con 側の変更なので、
  分けるなら epic 415 に起票する
- 回帰テスト: 上の行を ×60 で 10 行含む `markdown.Render` が一定時間内に返ることを、壁時計ではなく
  「chroma に渡った行の数」(入口の判定で素通しした数) で見る (`avoid-wall-clock-assertions.md`)

## 関連ファイル

- `src/tuikit/highlight/highlight.go` (`Code` / `Lang` / `Diff`)
- `src/tuikit/markdown/markdown.go` (`renderCode`。行を幅で切る) / `src/tuikit/markdown/render.go` (`paintLine`。`highlight.Lang` を呼ぶ)
- `src/pro-con/ui/activity.go` (`addActivity`) / `src/pro-con/ui/drawer.go` (`drawerPanel` / `drawerBody`)
- `src/glogx/issues/body.go` (`Body.Lines`)

## 進捗

- [ ] `highlight.Code` の入口に素通しの条件 + 閾値の実測
- [ ] 回帰テスト (変異で red を確認)
- [ ] pro-con の `addActivity` のキャッシュを、ここでやるか別 issue にするかを決める
