# 106 refactor: glogx のパッケージ横断の共有物が「値の写し + コメント同期」になっている

起票日: 2026-08-25 / priority: low

## 事実

glogx は `main` / `issues` / `usage` / `termsafe` / `widthenv` に分かれている。
`issues` と `usage` は `main` に import されるため **`main` を import できない**。
その制約に対して repo は既に 2 通りの答えを持っている:

- **leaf パッケージに切り出す**: `termsafe` の doc (`termsafe/termsafe.go:9-10`) が明示している —
  「main / issues の両方から使うため独立パッケージにしてある (main の非公開関数だと issues 側が
  呼べず、**コピーすると二重管理になる**)」。`widthenv` も同じ形
- **コピーして、コメントで「同じ」と宣言する** ← こちらが 3 箇所残っている

## コピー側の 3 箇所

| 対象 | 実体 | 備考 |
|---|---|---|
| 表示幅 `dispWidth` | `width.go:66` / `issues/wrap.go:16` / `usage/render.go:14` | 3 定義。`issues` と `usage` は素の `ansi.StringWidth`、`main` だけ fast-path (`fastDispWidth` + `symWidthTable`) を持つ |
| 子プロセスの猶予 | `usage/usage.go:72` の `SubprocessWaitDelay` | `issues` は使わず、`repoRootTimeout = 30s` を `issues/discover.go:42` に写して「import できないので値だけ揃える」と注記 |
| 基本 ANSI 色 | `render.go:14-20` (`ansiRed` 等) / `usage/render.go:21-23` (`cRed` 等) / `issues/render.go:24-25` | 同じ意味の色が 3 つの名前で定義されている |

いずれも**現時点で値は一致している**。壊れているのではなく、
**一致が「人がコメントを読んで揃える」ことに依存している**のが問題。

## なぜ直す価値があるか / どこまでやるか

- 幅は特に効く: `main` の `dispWidth` は fast-path が
  「`ansi.StringWidth` と幅の不一致 0 件」まで検証されている資産 (`issues/done/046-*`)。
  `issues` / `usage` はその恩恵を受けていない。**ただし両者が描画 hot path に載っているかは
  未計測**なので、統合の動機を「性能」に置くなら先に測ること (推測で寄せない)
- 色は最も低リスクだが最も drift しやすい (「警告の黄」が片方だけ 256 色に変わる等)
- `SubprocessWaitDelay` は [issue 105](105-bug-glogx-discover-missing-waitdelay.md) の
  置き場問題そのもの。105 を直すときに必然的に触る

## 提案 (trigger 待ち)

`termsafe` と同じ形の leaf パッケージを 1 つ作り、そこへ寄せる
(幅 / 子プロセス規律 / 基本色)。**先回りでは着手しない** — trigger は
「105 を直すとき」または「3 箇所目の写しを足したくなったとき」。

`main` の fast-path 付き `dispWidth` を leaf へ移すのは、
`fastDispWidth` の受理集合を守っている総当たりテスト
(`TestAcceptedSymbolsNeverCombineWithEachOther` 等) も一緒に移すこと。
移し忘れると「fast-path はあるが誰も検証していない」状態になり、今より悪化する。

---

## 進捗 (2026-08-26)

[issue 105](105-bug-glogx-discover-missing-waitdelay.md) の対応で **3 つのうち 1 つが解決した**。

- ✅ **子プロセスの猶予**: `src/glogx/subproc/` を新設し `WaitDelay` / `GitOpTimeout` を集約。
  `issues/discover.go` の値の写し (`repoRootTimeout`) は削除済み。
  この issue が言っていた「`termsafe` と同じ形の leaf パッケージ」が実在するようになったので、
  **残り 2 つの受け皿はもうある** (新規にパッケージを作る判断は不要)
- ⬜ **表示幅 `dispWidth`**: 3 定義のまま。`main` だけが fast-path を持つ。
  移すなら `fastDispWidth` の受理集合を守っている総当たりテストも一緒に移すこと
- ⬜ **基本 ANSI 色**: 3 定義のまま (`ansiRed` / `cRed` / `cGreen`)

trigger は据え置き (「幅か色で実際に drift が出たとき」または「4 箇所目の写しを足したくなったとき」)。

---

## 追記 (2026-08-27、leaky-abstraction 監査より)

**✅ とした「子プロセスの猶予」に、値のドリフトとは別の残りがある。**

値の正本は `glogx/subproc.WaitDelay` になり、`usage.SubprocessWaitDelay` は
`const SubprocessWaitDelay = subproc.WaitDelay` の**別名**にすぎない。それでも main 側の
**8 箇所** (`gitlog.go` / `github.go` / `external_commands.go` × 5 / `autobuild.go`) と
6 本の理由コメント (「理由は `usage.SubprocessWaitDelay` の doc」) は、いまだに
`usage` (= Claude Code の `/usage` と codex rate limit を取る領域パッケージ) を出典として
名指ししている。git / gh / tmux / clipboard の実行が `usage` を経由して猶予を得ている形。

また `subproc.CommandContext` は「新しい外部コマンド実行はこれを使うこと」と宣言しているが、
**実際の呼び出しは repo 全体で 1 箇所** (`issues/discover.go`) だけ。

- **壊れ方は compile error** (const alias なので値のドリフトは構造的に起きない)。
  106 の ✅ は嘘ではない。残っているのは**名前と依存の向き**だけ
- ⚠️ `waitdelay_discipline_test.go` は「WaitDelay が張られているか」しか見ないので、
  **この置換を守るテストは無い**。直すときに一緒に考えること

対応: 8 箇所を `subproc.WaitDelay` へ置換し、`usage.SubprocessWaitDelay` の別名は
`usage` 内部だけの利用に落とす (または削除)。
