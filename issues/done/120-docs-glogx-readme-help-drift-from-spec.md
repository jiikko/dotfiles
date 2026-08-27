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

---

## 対応 (2026-08-28)

**4 件とも実装を読んで裏を取ってから書き直した** (嘘を document しないため):

| 記述 | 実装での確認 |
|---|---|
| status viewer の `b` = push | `tui.go:handleKey` の status 分岐 (spec §3 / hint と一致) |
| `X` = codex update | `tui.go:1424` → `actModal.startCodexUpdate()` |
| `w` = 直近の警告/エラーをコピー | `tui.go:1431` → `lastWarning`。ghErr の sticky 警告も fallback で拾う |
| job 詳細の `v` | `tui.go:handleDetailKey` → `openJobLogInEditor()` (**job 一覧ではなく詳細ポップアップ**) |
| issues viewer の `u` | `issues_view.go:handleBodyKey` (**本文表示中のみ**。一覧では効かない) |

- `README.md` の `s` 行: 「`b` (push) は viewer 内では無効」→ spec §3 の内容 (開けた経緯つき) へ
- `README.md` のキー表: `X` と `w` の行を追加
- `README.md` の job 詳細ポップアップ節: 冒頭にキー一覧を追加 (`v` を含む)
- `options.go` (`--help`): `b` の記述を修正 / issues viewer の列挙に `u` を追加 /
  job 詳細ポップアップに `v` を追加

### 機械的な検査は入れなかった (判断と、入れるなら何か)

ドキュメントと実装の一致は静的検査に落としにくい。ただし**この repo は既に契約を持っている** —
`issues_view.go:hint` の doc が「hint は収まる範囲へ絞り、**絞られたキーは --help と README を
正本にする**」と書いている。つまり **hint に出るキーは必ず --help にある** (hint ⊆ help) が
成り立つはずで、これは hint 文字列が Go のリテラルなので機械的に検査できる。

今回入れなかったのは、hint 文字列から「キー」を切り出す規則 (`j/k:` `Ctrl-D` `Enter/h/q` 等の
表記ゆれ) を決める必要があり、docs の修正より大きくなるため。**次に同じ追従漏れを踏んだら
そこが起点**になる。

### 構造の提案 (ぼやき由来。実施していない)

`README.md` の「1 キー = 1 テーブルセルに数百字」という書き方が、今回の 4 件の共通の温床。
追加キーがセルに入らず散文へ逃げる (`X` がまさにその形だった)。viewer 系はセルに詰めず
`docs/*-viewer-spec.md` の §3 キー表へリンクだけ張ると正本が 1 つになるが、**体裁の変更なので
判断が要る**。ここでは既存の形のまま行を足すに留めた。
