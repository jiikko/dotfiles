# 181 ux: doctor の入口ドキュメントが 4 箇所欠けている (README のキー表 / --help / exit code / src/doctor の README)

起票日: 2026-09-02
重要度: **P2**
関連: [issues/163](163-audit-doctor-implementation-red-team.md) (体 6) / [`_claude/rules/new-tool-requires-entrypoint-docs.md`](../_claude/rules/new-tool-requires-entrypoint-docs.md)

## 対象

`src/glogx/README.md` のキー表、`src/glogx/options.go` の Usage、`bin/diskdoctor` / `bin/svcdoctor` の `--help`、`src/README.md`

## 何が起きるか

doctor は再利用される道具 (画面 + CLI 2 本) なのに、**存在を知っている人しかたどり着けない**。
実測 (実証済み。体 6 が `--help` を実際に叩き、grep で確認):

| 入口 | 状態 |
|---|---|
| `src/glogx/README.md` のキー表 | **`D` の行が無い** (doctor は `C` 行の脇役としてだけ言及) |
| `glogx --help` (`options.go` の Usage) | **D / doctor が 1 行も無い** |
| 唯一の入口 | 画面最下行の hint「D: doctor」(`tui.go` の hint 文字列) |
| `bin/diskdoctor --help` / `bin/svcdoctor --help` | 「削除しない」は書くが **exit 0/1/2 の意味を書かない** (main.go は exit で「検査できなかった」を伝える設計) |
| `src/README.md` | 「各プロジェクトの README を参照」と言うが **`src/doctor/README.md` が無い** (glogx / lockman / schedkeys / disassemble_excel には在る) |

## 対応案

- `src/glogx/README.md` のキー表に `D` (doctor を開く) と、画面内のキー (`Enter` 展開 / `y` / `Y` / `r` 再スキャン) を足す
- `options.go` の Usage に doctor を 1 行足す
- 両 CLI の `--help` に exit code の意味を書く (issue 177 で語彙を揃える変更と同じ commit でやる)
- `src/doctor/README.md` を作る (2 つの CLI の使い分け、glogx の doctor 画面との関係、削除は未実装であること)

## 受け入れ条件

- [ ] 4 箇所すべてが更新されている
- [ ] `src/doctor/README.md` に「diskdoctor と svcdoctor の使い分け」が 1 行で書かれている
