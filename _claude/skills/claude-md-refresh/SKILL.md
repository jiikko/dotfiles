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
  | xargs -n1 dirname | sort -u \
  | while IFS= read -r d; do
      case "/$d/" in */issues/*|*/issue/*)
        # issue 運用の置き場 (直下に NNN-*.md を持つ issues/) とその配下だけ外す。同名の Go package 等は残す
        t=$(printf '%s\n' "$d" | sed -E 's#^((.*/)?issues?)(/.*)?$#\1#')
        [ -n "$(find "$t" -maxdepth 1 -name '[0-9][0-9][0-9]-*.md' -print -quit)" ] && continue ;;
      esac
      printf '%s\n' "$d"
    done
```

- README だけのディレクトリも対象に入れる (CLAUDE.md を起点にすると、README しか持たない module が漏れる)
- issue 運用の置き場 (直下に `NNN-*.md` を持つ `issues/` / `issue/`) は配下ごと外す。issue の本文と運用の README は issue-writeback / issue-sync の担当
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

対象が 5 ディレクトリを超えるなら、sonnet の read-only サブエージェント (Explore) に照合を任せ、**1 体ずつ直列に**回す。

- 組は `wc -l` で文書の行数を見て均す。数百行の README は 1〜2 ディレクトリで 1 組、短い文書は 5〜7 ディレクトリをまとめる
- 指示は下の雛形を使う。`<対象>` と `<背景>` だけを埋める。背景には、直近の rename・移動・module path の変更など、
  照合する側が知らないと古い記述を見逃す事実を書く (無ければ行ごと消す)
- 返ってきた一覧は **main が根拠を確かめてから**採る (`~/.claude/rules/subagent-model-tiering.md` の検閲の観点)。
  特に「〜は存在しない」は、grep の取りこぼしを疑って main が数え直す
- 次の組は、前の組の指摘を確かめてから起こす (確かめる間に修正を始めてよい。修正するファイルと次の組が読むファイルが重ならない範囲で)

```
読み取りのみ。何も編集しないこと。<repo の絶対パス> の次の文書を、実体 (コード・コマンド・構成) と照合して乖離を探してください。

対象: <ディレクトリ (CLAUDE.md / README.md のどれか)>
背景: <直近の rename・移動・path の変更。無ければこの行を消す>

照合の仕方:
- 文書の検証できる主張を拾い、実体で確かめる: パスと構成表・索引 (ls / git ls-files。索引なら「載っているが実在しない」と
  「実在するが載っていない」の両方向を数える)、関数・型・定数のシンボル名と数値 (grep で**定義側**を確認。参照だけのヒットで実在としない)、
  コマンド・flag・subcommand・キー操作・make target (Makefile や CLI・キーの定義のコードを読む)、「X に依存」「X を取り込む module の一覧」
  (go.mod / package.json 等を数える)、workaround の警告 (根本原因の修正が入っていないか git log で見る)、件数・一覧 (数え直す)
- 逆向き: 実体にあって入口の文書に載っていない利用者向けのもの (subcommand・flag・キー・make target・公開 API・道具) も探す
- 同じ文書の別の節どうし、同じディレクトリの CLAUDE.md と README で、同じことを違って書いていないかも見る
- 規範の文書 (作業の仕方を定めた CLAUDE.md 等) は、規範の妥当性を判定しない。参照しているパス・コマンド・検査の実在だけを見る
- 印象で「古そう」と判定しない。根拠を示せないものは出さない
- 「〜は存在しない」と判定する前に、rename 先・別ファイル・別 module への移動を git log -S / grep で探す

出力: 乖離ごとに 1 項目で、
- 文書 (パス) と該当箇所の引用 (短く。行番号も添える)
- 実体の根拠 (file:symbol、コマンドの出力、git log の commit subject)
- 種類: 構造的 / 意味的 / 記載漏れ
- 確信度: 高 / 中
乖離が無い文書は「乖離なし」と 1 行。最後に、照合した主張のおおよその件数を文書ごとに書く。
```

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
