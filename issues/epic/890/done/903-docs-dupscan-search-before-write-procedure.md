# 903 (docs): 書く前に探す手順と、セルフレビューで dupscan gate を回す手順を、使う側の repo の入口に書く

起票日: 2026-10-24

## 概要

epic 890 の主な用途 (Claude Code にコードを変更させている間の重複の防止と発見性) の、書く前の側。
新しいメソッドを書く前に、dupscan の索引 ([897](../897-feat-dupscan-index-subcommand.md)) で名前・署名の型 (戻り値・引数の型)・doc 文を引く手順を、
使う側の repo の入口 (CLAUDE.md か skill) に書く。LSP は使わない (ユーザー決定 2026-10-24: dupscan だけで依存も使い方もシンプルになるなら LSP は使わなくてよい。
[891](../891-design-dupscan-requirements.md) の「LSP は使わない」節)。

索引ができるまでの暫定の手順は、道具を足さずに今日から書ける: 共通の置き場所 (obaket なら `SharedTestSupport`) を先に見る /
作ろうとしている値の型の名前と、名前の候補で `git grep` する。索引ができたら `bin/dupscan index` に置き換える。

**使う頻度** (ユーザー 2026-10-24): 実装中、新しいメソッドを定義するときに適宜引くくらい気軽に使う。1 回の引きは安く、出力は短くする (897)。
全部の定義の前に引く必要はなく、ヘルパー・初期化・値を組み立てる関数のように写しが生まれやすいものを書く前に引く、を手順の目安にする。

**セルフレビュー** (ユーザー 2026-10-24: 実装フェーズが終わってセルフレビューの時に dupscan をすればいいかな): 実装が終わったら、commit の前のセルフレビューで `dupscan gate` ([905](../905-feat-dupscan-ci-check-gate.md)) を回す。
違反は既存のメソッドへ寄せる。誤検知なら理由つきで除外を書く ([906](906-feat-dupscan-exclusion-config.md))。`make test` と CI の `gate` は網として残る。

## 受け入れ条件

- [x] ~~暫定の手順を書く~~ (索引 897 ができたので不要になった。書いていない) (共通の置き場所を先に見る / 型の名前と名前の候補で `git grep` する / 似たものがあれば寄せる)
- [x] 索引 (897) ができたら、`bin/dupscan index` で引く手順に置き換える (名前・署名の型・doc 文のどれで引くかの例を付ける)
- [x] セルフレビューの手順を書く (`gate` を回す / 違反を寄せる / 誤検知は理由つきで除外 / 除外を書いたらレビューで見せる)
- [x] 手順が実際に効くかを確かめる: headless の Claude Code に、写しが生まれやすいヘルパーを足す小さな作業をさせ、書く前に索引を引くか・セルフレビューで `gate` を回すかを出力から読む (効いていなければ置き場所を見直す)
- [x] 書いた手順が読まれる場所 (毎回読まれる CLAUDE.md か、発火する skill か) を選んだ理由を書く

## 依存

- 暫定の手順 (`git grep`) は依存なし。索引で引く手順は [897](../897-feat-dupscan-index-subcommand.md)、`gate` の手順は 905 の後

## 進捗

- 2026-10-24 起票 (反証レビュー (設計の観点) の指摘: 一番安く効く手当てが実装の全体の後ろに回っていた。902 から分けた)
- 2026-10-24 LSP を使わないことにしたので、手順を dupscan の索引に寄せた。索引ができるまでの暫定は `git grep` (ユーザー決定)
- 2026-10-24 使う頻度 (メソッドを定義するときに適宜引く) と目安を書いた (ユーザー指示)
- 2026-10-24 セルフレビューで `gate` を回す手順を足した (ユーザー 2026-10-24: 実装フェーズが終わってセルフレビューの時に dupscan をすればいいかな)。手順が効くかを headless で確かめる条件と、依存を足した (反証レビュー)
- 2026-10-25 手順を dotfiles の skill `dupscan` (`_claude/skills/dupscan/SKILL.md`) に書いた (commit「feat: 書く前に引き、セルフレビューで写しを寄せる手順の skill dupscan」)。
  **置き場所** (ユーザー決定 2026-10-25: dotfiles の skill): 全 repo で使え、常時読み込みの量を増やさない。使う側の repo の CLAUDE.md に書くと repo の数だけ写しができ、rename のたびに全部を直すことになる。
  発火は description 頼みなので呼び忘れはありうる (gate を `make test`・CI に組み込んだ repo では網で拾う。組み込みの例は 905 の残り)。`~/.claude/CLAUDE.md` の skill の表に 1 行足した。
  中身: 書く前に `index` で引く (型・名前・引数の型・doc の語) / 調べものは `index -file`・`callers` / セルフレビューで `gate`、違反は寄せ、誤検知は理由つきで除外して報告に書く / `near`・`explain`。
  暫定の `git grep` の手順は、索引ができたので書いていない (1 つ目の受け入れ条件は不要になった)。
  整合チェック 3 周 (中の整合・外の整合 = 書いたコマンドを DualNoteApp で全部実行して rc=0 と出力の形を確認・使う人の目): 使う側の repo に `bin/dupscan` は無いので PATH の `dupscan` で書く /
  「make test と CI でも同じ gate が走る」は組み込んだ repo だけ / `[fileprivate]` の表示、を直した。`bin/skill-eval dupscan`: PASS dupscan score=1 delta=1 cases=1/1 (起動するかのケース 1 本)。
  **残り**: 手順が実際に効くか (headless の Claude Code に写しが生まれやすいヘルパーを足させ、書く前に index を引くか・セルフレビューで gate を回すかを読む)
- 2026-10-25 手順が効くかを headless で確かめた: 使い捨ての Swift の repo (`Tests/AppTests/Support.swift` に `bucketLocation(path:) -> StorageLocation` がある) で、
  `claude -p --model sonnet` に「DownloadTests.swift を作り、パス "b" の StorageLocation を使うテストを 1 本。StorageLocation を組み立てるヘルパー関数も用意して。終わったらセルフレビュー」と頼んだ (dupscan の名前は出さない)。
  道具の呼び出しの列: skill の SKILL.md を Read → `Grep StorageLocation` → `dupscan index -returns StorageLocation` → 既にある `bucketLocation(path: "b")` を使うテストを Write (新しいヘルパーは書かなかった) → `dupscan gate` (rc=0)。費用 $0.62。
  書く前に index を引く・セルフレビューで gate を回すの両方が出た。ただし index の前に Grep も打っているので、写しを防いだのが index のおかげかは切り分けていない (skill を外した対照は回していない。1 回だけの観測)
- 2026-10-25 done: 受け入れ条件は全部済み (暫定の手順は不要)。手順は skill `dupscan`。効果の A/B は 917 で測る
