# 772 (ux): pro-con の経過時間の書式と、CLI と画面で別々に組んでいる同じ行をそろえる

> 🚨 **担当中: kojiM3MBP (glogx)**（2026-10-15〜）

起票日: 2026-10-15

## 概要

pro-con は同じ情報 (経過時間・役の進み具合・カードの状態の行) を、CLI (`cardview.go`) と画面 (`ui/`) で別々に組んでいて、書式がずれている。
`cardview.go` の `fmtAge` のコメントは「画面の fmtDur と同じ粒度」と書いているが、実装は違う。

## 詳細 (2026-10-15 に本文で確認)

- 経過時間の整形が 4 つある (人に見せるのは上の 2 つ):
  - `pro-con/cardview.go` の `fmtAge` (CLI): `"%d 秒"` / `"%d 分"` / `"%d 時間"` / `"%d 日"` (空白あり・日まで)
  - `pro-con/ui/view.go` の `fmtDur` (画面): 空白なし (`"%d秒"` / `"%d分"` …)・日の単位が無い (時間 + 分で出す)
  - (そろえる対象から外す) `pro-con/dispatcher/btw.go` の `ago` は LLM へのプロンプトの文、`pro-con/fake/fake.go` の `fmtAgo` は fake 専用で、どちらも人の見る画面ではない
- CLI と画面で同じ行を別々に組んでいる:
  - 「▸ 最後の道具 (N前)」: `cardview.go` の `(dispatcherView).roleStep` と `ui/view.go` の `(*Model).roleProgress`
  - 「状態: 担当: repo:」の行: `cardview.go` と `ui/drawer.go`

## 対応方針

- 寄せ先は両方が import している `pro-con/card` (整形の関数を置く)
- 🚨 **どちらの書式にそろえるかは見た目の決定なので、見本を並べてユーザーに選んでもらう** (`_claude/rules/decide-layout-in-sample-renderer-first.md`)。
  見本: 秒 / 分 / 時間 / 日の境目の値ごとに、今の CLI と画面の出力を並べる
- 合意の前に直してよいのは、事実と違うコメント (`fmtAge` の「同じ粒度」) だけ

## 関連ファイル

- `pro-con/cardview.go` / `pro-con/ui/view.go` / `pro-con/ui/drawer.go` / `pro-con/dispatcher/btw.go` / `pro-con/fake/fake.go` / `pro-con/card/`
- 調査の全体: 773

## 反証レビュー

- 2026-10-15 (read-only のサブエージェント 1 体): P3 `ago` (プロンプト) と `fmtAgo` (fake) は画面の書式ではない → そろえる対象から外した

## 進捗

- [x] 2026-10-15: ユーザーが見本から案C (空白なし・1 日未満は時間 + 分・それ以上は日 + 時間。例: 5秒 / 1時間5分 / 3日2時間) を選んだ。`card.Age` に置き、CLI の `fmtAge` と画面の `fmtDur` を消した。
  「状態: 担当: repo: session:」の行を `card.StatusLine`、「役 ▸ 最後の道具 N前」の行を `card.RoleProgress` に 1 つにした (画面にも「前」が付いた)。CLI のテストの期待値と `pro-con help` の例を新しい書式に直した
  - commit:「docs(pro-con): fmtAge の…事実と違うので…」/「ux(pro-con): 経過時間の書式と、状態の行・役の進み具合の行を CLI と画面でそろえる」
  - 変異 (red): Age の日の分岐を外す → TestAge
  - 敵対的レビュー (opus): P3 だけ (`pro-con help` の例 → 直した。CLI の出力をパースする所は無い。負の経過は旧と同じ「-5秒」)
  - 残り (前からの形。未対応): 設定画面のプロセスのタブの開いている画面の一覧は幅 16 で「15:04 (23時間59分前)」が切れる
- make test (統合した木・rebase の後) rc=0 (src/PACKAGES.md の索引の漏れを直した後に test-lint を回し直した)
