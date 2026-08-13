# parallel-each: 表示幅を rune 数で数えているため日本語・絵文字で桁がずれる

起票日: 2026-08-13

`src/parallel-each/format.go` が端末の表示幅を**自前計算**している。glogx が `width.go` で
「表示幅の単一情報源」として解決済みの問題を、同じ repo の別プログラムが踏んでいる。

## 該当箇所

- `visibleLen(s string) int` — ANSI エスケープの読み飛ばしを手書きし、**1 rune = 1 桁**で数える
- `truncate(s string, max int) string` — `[]rune` に変換して **rune 数**で切り、`…` を付ける

どちらも「文字数」であって「表示幅」ではない。CJK（全角）・絵文字（VS16 付き）・国旗は
端末では 2 桁を占めるので、幅計算が実際の描画と食い違う。

## 何が問題か

- **列が揃わない**。日本語を含むラベル・パス・コマンド出力が並ぶと、罫線や列区切りが実際の
  描画位置とずれる（`visibleLen` で桁を数えて詰め物を決めている箇所すべて）
- **切り詰めが効きすぎる / 足りない**。`truncate` は全角 10 文字を「10 桁」と判断するが実際は
  20 桁で、枠を越える
- glogx 側は同じ罠を踏んで対処済みで、`src/glogx/width.go` 冒頭に「別ライブラリで測ると
  両者が食い違う文字（VS16 付き絵文字・国旗は runewidth=1 だが端末=2）で桁がずれる」と
  実測つきで記録している。**parallel-each はその知見の外にいる**

## 対応案

`github.com/charmbracelet/x/ansi` を使う。⚠️ **モジュール機構の追加は要らない**:
`src/parallel-each/go.mod` に `x/ansi v0.10.1` が既に indirect で入っているので、import 1 行で済む
（glogx と parallel-each は別モジュールなので共有パッケージを作る話にはしない — 上流ライブラリを
共有し、薄い wrapper を各パッケージで再宣言する方針は `src/glogx/issues/wrap.go` が前例）。

- `visibleLen` → `ansi.StringWidth`（ANSI は幅 0、grapheme クラスタ単位）
- `truncate` → `ansi.Truncate(s, max, "…")`

幅モデルを glogx と揃える理由は `src/glogx/width.go` 冒頭が一次情報（GraphemeWidth を選ぶ根拠と
層ごとの実測値）。

## 受け入れ条件

- 全角・絵文字・国旗を含む文字列で、`visibleLen` の戻り値が端末の実表示幅と一致する
- `truncate` が表示幅基準で切る（全角 10 文字 = 20 桁を `max=20` で切らない）
- 上を固定するテスト。⚠️ ASCII だけのケースは変異で落ちないので、**全角・VS16 付き絵文字を
  必ずケースに入れる**（ここを外すと「テストは green だが桁はずれたまま」になる）

## 経緯

2026-08-13、glogx の「ドメインとして切り出せていない箇所はないか」の調査中に発見。
調査の結論は「glogx 側は既に適切に切り出し済み・共有パッケージを作る条件（実在する第二消費者）を
満たさない」で、その過程で parallel-each 側のこの実装が見つかった。重複の解消ではなく
**parallel-each 単体のバグ**として扱う。

## 非目標（将来の audit が再指摘しないように）

`glogx.formatDuration` と `parallel-each.formatDur` は**重複ではない**。前者は秒未満を持たず 0 以下で
空文字を返す（CI の所要時間表示）、後者は `ms` と `%.1fs` を出す（コマンド実行時間）。
統一するとユーザー可視な出力が変わるので、別実装のままが正しい。
