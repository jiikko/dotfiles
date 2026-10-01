---
name: claude-md-refresh
version: 1.0.0
description: ディレクトリ・レイヤーごとに置いた CLAUDE.md と README.md が実体 (コード・コマンド・構成) とずれていないかを点検し、裏の取れた乖離だけを直す。引数で対象のディレクトリを任意に指定できる (省略時は repo 全体)。「CLAUDE.md を refresh して」「README が古くないか見て」「claude-md-refresh」「/claude-md-refresh」で発火。issues/ の更新漏れは issue-writeback / issue-sync の担当で、この skill は扱わない。
---

# CLAUDE.md Refresh

ディレクトリに置いた文書 (CLAUDE.md / README.md) を実体と突き合わせ、ずれていれば直す。
保守の規範 (何を残し、何を書かないか) の正本は `~/.claude/rules/claude-md-maintenance.md` で、この skill はそれを
**明示起動の一括点検**として回す手順を持つ (ルールの「定期レビューはしない」の例外。ユーザーが起動したときだけ回す)。

## 引数

```
/claude-md-refresh                    # repo 全体
/claude-md-refresh src                # src 配下 (再帰)
/claude-md-refresh src/glogx src/doctor
```

## Step 1: 対象を列挙して先に示す

対象 = 指定したパス配下 (省略時は repo root) で、**tracked な `CLAUDE.md` か `README.md` の少なくとも一方を持つディレクトリ**。
そのディレクトリにある両方を点検する (片方しか無ければ在る方だけ)。

```sh
git ls-files -- "${@:-.}" | grep -E '(^|/)(CLAUDE|README)\.md$' \
  | grep -vE '(^|/)(testdata|fixtures?|vendor|third_party|node_modules)/' \
  | xargs -n1 dirname | sort -u
```

- README だけのディレクトリも対象に入れる (CLAUDE.md を起点にすると、README しか持たない module が漏れる)
- 列挙が空なら、引数のパスが合っているかを伝えて終える
- 件数とディレクトリの一覧を、照合に入る前にユーザーへ出す

## Step 2: 文書の主張を実体と照合する

ディレクトリごとに、文書の**検証できる主張**を拾って実体で確かめる。読んだ印象で「古そう」と判定しない。

| 主張の種類 | 確かめ方 |
|---|---|
| ファイル / ディレクトリのパス、構成表 | `ls` / `git ls-files` |
| 関数・型・定数などのシンボル名 | grep (定義側を見る。参照だけのヒットで実在としない) |
| コマンド・flag・subcommand・make target | `--help` / Makefile / CLI の定義のコード |
| 「〜は X に依存」「現状の foo に合わせている」 | 依存先の現コードと git log |
| workaround と、その理由の警告 | 根本原因の修正が入っていないか (git log / issue) |
| 件数・一覧 (「N 個の module」等) | 実体を数え直す |

逆向きも見る: **実体にあって入口の文書に無いもの** (新しい subcommand・make target・module・利用者が叩く道具)。
入口の文書に載っていない道具は使われない (`~/.claude/rules/new-tool-requires-entrypoint-docs.md`)。

乖離は `claude-md-maintenance.md` の「乖離の検出シグナル」に沿って、構造的 (物理的に無い) / 意味的 (意味が変わった) に分ける。

### ディレクトリが多いとき

対象が 5 ディレクトリを超えるなら、sonnet の read-only サブエージェントに照合を任せ、**1 体ずつ直列に**回す
(1 体あたり数ディレクトリ)。指示は「乖離ごとに、文書の該当箇所・実体の根拠 (file:symbol / コマンドの出力)・構造的か意味的か を返す。
直さない」。返ってきた一覧は **main が根拠を確かめてから**採る (`~/.claude/rules/subagent-model-tiering.md` の検閲の観点)。
特に「〜は存在しない」は、grep の取りこぼしを疑って main が数え直す。

## Step 3: 裏の取れた乖離だけ直す

- **直すのは根拠を確かめた乖離だけ**。確信が持てないものは書き換えず、報告の「確かめられなかった」に回す
- CLAUDE.md は **Why (制約・過去の事故・前提) を残す**。What の目次や、コードを読めば分かる説明を足さない
- README は利用者向けの入口。使い方・コマンド・flag の記述を実体に揃え、実体にある道具が載っていなければ足す
- 警告を消すのは、その警告の根拠 (workaround が必要だった原因) が無くなったことを確かめたときだけ
- 位置は file 名 + symbol 名で書く。行番号を書かない
- 親の CLAUDE.md にある規約を子へ写さない (継承前提。写すと片方の更新漏れが起きる)
- 中身が「コードを読めば自明」だけになった CLAUDE.md は、消す候補として報告する (消すかはユーザーが決める)
- 文書の外 (コード・テスト) は直さない。コード側の不具合に見えたら報告に書く

## Step 4: 報告と commit

ディレクトリごとに 1 行で出す:

```
| ディレクトリ | 結果 | 内容 |
|---|---|---|
| src/glogx | 直した | README: 廃止した --foo flag の記述を削除 / CLAUDE.md: rename 後の symbol 名に追従 |
| src/doctor | 乖離なし | |
| src/pro-con | 確かめられなかった | CLAUDE.md の「X は Y に依存」— 依存先が見つからず、削除か rename か判断できない |
```

- 件数を数えてから書く (「N 件直した」は `git diff --stat` で数える)
- commit は自分が書き換えた文書だけを pathspec で明示する。repo に worktree や commit の規約があればそれに従う
