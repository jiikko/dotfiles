# 510 (feat): pro-con を外から動かす Claude 向けの入口 — `pro-con help <話題>` と薄い skill

起票日: 2026-09-26

親: [415](415-design-claude-pm-worker-orchestration.md)

## 概要

pro-con を使って開発する Claude (カードを積む・質問に答える・止まったものを調べる側。PM / PG / 取り込みの係ではない) が、
毎回 README を読み直さないと用語 (PM・PG・取り込みの係・見張り・dispatcher・レーン・受付の箱) も使い方もデバッグの入口も分からない。
ユーザーの依頼: 「用語が伝わるようになって、デバッグ、どういう使い方をしていいかをロードするための skill が欲しい」。

今の入口:

- `pro-con --help` は 1 行だけ (「詳しくは README」)
- README (384 行) は人と実装者向けで、読み込ませるには長く、運用の判断 (どの場面で何を使うか・やってはいけないこと) がまとまっていない
- `card guide` は PM (既定) と取り込みの係 (`--integrator`) に渡す指示書で、外から動かす側向けではない (PG 向けの guide は無い)

## 決めたこと (2026-09-26、ユーザーと合意)

- **正本は pro-con 側に置く**: `pro-con help <話題>` を足す。コマンド・状態の置き場はバイナリと同じ commit で直るので、skill に写すとずれる
- **skill は薄い入口にする** (40 行前後): 読み込ませる条件 (description)・用語の 1 行の説明・やってはいけないこと・`pro-con help <話題>` への案内だけ
- `card guide` (PM・取り込みの係に渡す指示書) は今のまま。skill はそれを置き換えない

## 対応方針

1. `pro-con help` に話題を足す (話題名は実装で決めてよい。最低限この 3 つ):
   - 用語: 役 (PM / PG / 取り込みの係 / テストの係 / 見張り / 要約の係)・dispatcher・受付の箱・レーンと「人の番」
   - 使い方: 依頼の出し方 (画面の `+` / `card order` / `card add`)・質問への答え方 (`card answer` / 回答フォーム)・`--join` と `--view`・カードの削除と片付け
   - デバッグ: 状態の置き場の場所と中身・`ps` / `du` / `log` / `card show`・クラッシュ後の復旧・ライブアップグレード (画面の ctrl+r / dispatcher の自動の入れ替え)
   - 引数なしの `pro-con help` は話題の一覧を出す。`--help` の 1 行からも `pro-con help` を案内する
2. README と中身を二重に持たない。README の該当の節を `help` へ寄せ、README からは `pro-con help <話題>` を指す (どちらを正本にするかは実装で決めてよいが、同じ文を 2 か所に書かない)
3. skill `_claude/skills/pro-con/SKILL.md` を足す。description には会話に出る語 (pro-con・カード・PM・PG・dogfooding・レーン・dispatcher) を入れる
   - やってはいけないことの例: 質問待ちのカードを `card delete` すると質問が消える / `cards.json` を手で書かない (書くのは dispatcher だけ) /
     PG の worktree を手で消さない (`pro-con worktree clean` を使う)
4. 入口の更新 (`new-tool-requires-entrypoint-docs.md`): `_claude/CLAUDE.md` の「スキルファイル参照」の表に 1 行、pro-con の README に skill の存在を 1 行

## 受け入れ条件

- [x] `pro-con help` が話題の一覧を出し、各話題が引ける。知らない話題は一覧を出して rc≠0
- [x] 用語・使い方・デバッグの中身が README と二重になっていない
- [x] skill が 60 行以内で、コマンドの一覧・状態の置き場のパスを写していない (`pro-con help` を指す)
- [x] 新しいセッションで skill が読み込まれることを headless で確かめる (`claude -p --model haiku` に「pro-con の PG とは何か」を聞き、skill を読んだ答えが返るか。
      読み込まれない cwd でも 1 回回して A-B にする。dotfiles の `.claude/rules/worktree-per-session.md` の手順)
- [x] `_claude/CLAUDE.md` の表と README に入口がある

## 関連ファイル

- `src/pro-con/main.go` (`--help` の 1 行) / `src/pro-con/README.md` / `src/pro-con/cardcmd.go` (`card guide`)
- `_claude/skills/` / `_claude/CLAUDE.md` (スキルファイル参照の表) / `scripts/claude_links.sh` (skill の link)

## 反証レビュー (2026-09-26、sonnet・読み取りのみ)

- 採った: 「`card guide` は PG にも渡す」は誤り → PM と取り込みの係の 2 つだけに直した (`cardcmd.go` の `case "guide"`)
- 反証できなかった: `--help` は 1 行だけ / README 384 行 / 質問待ちのカードの `card delete` で質問が受け付けられなくなる (`store.go` の `transition` が `Deleting()` を拒む) / cards.json を書くのは dispatcher だけ / `worktree clean` がある / skill は dir 単位で link される / 重なる issue は無い

## 進捗

- 2026-09-26 (C-068): `pro-con help` を足した (`src/pro-con/helpcmd.go`)。話題は `terms` (用語) / `usage` (使い方) / `debug` (デバッグ)。
  日本語の別名でも引ける。本文の正本は `src/pro-con/help/*.md` (embed)。役・レーン・人の番は画面の `?` の表と同じ正本
  (`ui.RoleMeanings` / `card.State.Meaning` / `ui.HumansTurnMeaning`) から差し込み、md に写さない。役の表に「要約の係」を足した (`?` の表にも出る)
  - README は冒頭で `pro-con help` を正本として指すだけにし、同じ文を書かない。`--help` の 1 行からも案内する
  - 検査: `helpcmd_test.go` (一覧・知らない話題で rc=2・全話題が引けて目印が残らない・terms が正本と一致・usage / debug に書いた
    `pro-con card` / `pro-con log` がパーサに通る)。目印の置き換えを壊す / rc を 0 にする mutation で赤くなるのを確かめた
- skill `_claude/skills/pro-con/SKILL.md` (38 行)。`_claude/CLAUDE.md` のスキルファイル参照の表に 1 行、README に 1 行
- headless の A-B (2026-09-26、`claude -p --model haiku --allowedTools Skill Read`、「pro-con の PG とは何か」):
  master へ載る前なので、skill を project の `.claude/skills` に写した隔離 dir と写さない dir で比べた
  - 写した dir: `Skill {"skill":"pro-con"}` を呼び、「PM が分けたカード 1 枚ずつを自分の worktree で実装する役割」と答えた (`skill: pro-con`)
  - 写さない dir: 「定義が見当たらない」(`skill: なし`)
  - 🚨 `--allowedTools` は可変長で後ろの質問文を飲み込む (`Input must be provided …` で空振りした)。質問は stdin で渡す
- 本番の A-B (2026-09-27、`~/.claude/skills/pro-con` の link 経由。空の一時 dir から `claude -p --model haiku --output-format stream-json`、質問は stdin):
  - A (`--allowedTools Skill Read`) を 3 回: 2 回は `Skill` の道具を使わず `Read` で `~/.claude/skills/pro-con/SKILL.md` を読み、「カード 1 枚ずつを自分の worktree で実装する役割」と答えた。
    1 回 (質問に「1 文で答えて」を付けた回) は読まずに「SKILL.md を見ないと答えられない。確認してよいか」と聞き返した
  - B (`--disallowedTools Skill`、Read は可): 読まずに skill の説明文だけから推して「PG は Program / Project のマネージャー役」と誤った
  - 記録だけ: A の 2 回とも PG を略語と思い込んで展開を作った (「Programmer」「Product Generator」)。役割の説明は合っている。skill に略語の説明は無い

