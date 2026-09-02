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

- [x] 4 箇所すべてが更新されている
- [x] `src/doctor/README.md` に「diskdoctor と svcdoctor の使い分け」が 1 行で書かれている

## 対応 (2026-09-02, `6c87323`)

起票時の 5 つの主張はすべて実測で確認できた (README のキー表に `D` 無し / `options.go` に
doctor 0 件 / 両 CLI の `--help` に exit code 無し / `src/doctor/README.md` 不在)。

| 入口 | 対応 |
|---|---|
| `src/glogx/README.md` | `C` の直前に `D` の行を追加。画面内のキー (`Enter` 展開 / `y` / `Y` / `r` / `D`・`q`・`Esc`) と「削除しない」を併記し、`src/doctor/README.md` を指した |
| `src/glogx/options.go` | Usage に `D` の項を追加。`glogx --help` で実際に出ることを実行して確認 |
| `bin/diskdoctor --help` / `bin/svcdoctor --help` | 終了コード節を追加 (下記の非対称つき) |
| `src/doctor/README.md` | 新設。2 CLI の使い分け表・終了コード表・glogx 画面との関係・パッケージ構成 |

`src/README.md` は変更していない。「使い方・設計は各プロジェクトの README を参照」という
記述は、`src/doctor/README.md` を作った時点で成立するため (指していた先が無いことが問題だった)。

### 終了コードは実装どおりに書いた (177 と重ねなかった)

対応案は「issue 177 と同じ commit で」としていたが、**現状を正確に書くことは 177 を待たずにできる**
ので分けた。実装を読んで書いた実態:

| | 0 | 1 | 2 |
|---|---|---|---|
| `diskdoctor` | 一覧を出せた (候補の有無で変わらない) | 出力に失敗 (`-json` のエンコード) | 引数が不正 / 走査できなかったエントリがある |
| `svcdoctor` | 候補なし | 候補あり (または home 解決・出力の失敗) | 引数が不正 / 診断できなかったものがある |

`--help` と README には既知の非対称も明記し、issue 177 を指した:
「候補あり」が diskdoctor は 0 / svcdoctor は 1、かつ **`svcdoctor -json` は候補・未診断があっても
0 を返す** (JSON を書いた時点で return するため、終了コードの switch に到達しない)。

### 実測メモ

- `diskdoctor` の rc は run ごとに変わる (0 の回と 2 の回がある)。2 は failed エントリが出た回で、
  `blocked` (guard による対象外) では 2 にならない — `blocked` と `failed` は別扱い
- 検証: `make -C src/doctor lint` / `test`、`make -C src/glogx lint`、`go test ./...` すべて green
