# 120 docs: README と `--help` が status viewer の `b` を「無効」と書いているが、実装は push 確認を開く

起票日: 2026-08-27 / 出典: ux 監査 / priority: medium

## 事実

4 系統の記述のうち **2 つだけが古い**:

| 出典 | 内容 |
|---|---|
| `docs/status-viewer-spec.md` | 「**push は `b`** (ユーザー要望 2026-08-07)。当初は遮断していたが開けた」 |
| 実装 (`tui.go:handleKey` の status viewer 分岐) | `key == "b"` → push 確認を開く |
| 実行時 hint (`status_view.go:hint`) | `… b: push  p: pull  U: usage  q: …` |
| **`src/glogx/README.md`** | 「`b` (push) は viewer 内では**無効** (staging から remote 操作へ滑る導線を作らない)」 |
| **`src/glogx/options.go` (`--help`)** | 「push (b) は viewer 内では**無効**。」 |

commit `def8582` が触ったのは `docs/status-viewer-spec.md` / `status_view_test.go` / `tui.go` の
3 ファイルのみで、**README と options.go に追従していない**。「意図的に据え置いた」という
記述はどこにも無い (commit message の「(u の案内は据え置き)」は `u` の話)。

## ユーザーに何が見えるか

README / `--help` を読んで「viewer 内から remote は触れない」と理解した人が `s` → `b` を押すと、
**push 導線が無いはずの画面から push 確認が始まる**。同じアプリの hint 行は `b: push` と
正しく出しているので、アプリ内で矛盾している。

## 併せて: キー表の記載漏れ 3 件 (同じクラス)

| 漏れ | 状況 |
|---|---|
| `w` (直近の警告/エラーをコピー) | `--help` にはあるが **README のキー表にも散文にも無い** |
| `X` (codex update) | `--help` にはあるが README はキー表に行が無く散文に 1 回だけ |
| `v` (job 詳細ログを nvim で開く) | **README にも `--help` にも無く**、実行時 hint にだけある |
| `u` (URL ピッカー) | README にはあるが **`--help` の issues viewer 行に無い** |

`issues_view.go:hint` の doc は「hint は収まる範囲へ絞り、**絞られたキーは --help と README を
正本にする**」と契約を明記している。つまり README / `--help` 側の欠落は幅の都合ではなく契約違反。

## 対応

- README の `s` 行と `options.go` のヘルプを spec §3 と同じ内容 (「`b` で push (y/N 確認)」) に直す
- `w` / `X` を README のキー表に行として足す。`v` を README の job 詳細節と `--help` に足す。
  `--help` の issues 行に `u` を足す

## 構造的な提案 (ぼやき由来。判断が要る)

`README.md` の「1 キー = 1 テーブルセルに数百字」という書き方が、上記 4 件の共通の温床に見える
(追加キーがセルに入らず散文へ逃げる)。viewer 系はセルに詰めず
`docs/*-viewer-spec.md` の §3 キー表へリンクだけ張る形にすると正本が 1 つになる。
**これは体裁の変更なので、直す人が判断すること。**
