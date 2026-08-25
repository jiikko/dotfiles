# `.gitignore` の `CLAUDE.md` がレイヤー CLAUDE.md を飲み込み、規約が共有されない

起票日: 2026-08-26
種別: bug
優先度: **P1** (規約が 1 台のマシンにしか存在しない。失うと復元できない)

## 確認できた事実 (2026-08-26)

`.gitignore:12` に無条件の `CLAUDE.md` がある (前後にコメントは無く、意図が読めない):

```
CLAUDE.md
Brewfile.lock.json
```

git のパターンは**どの階層の `CLAUDE.md` にもマッチする**ため、追跡状況が割れている:

| ファイル | 追跡 | 中身 |
|---|---|---|
| `_claude/CLAUDE.md` | **される** | 2026-02-17 の `3191b4b` で追加済み (追跡済みファイルに ignore は効かない) |
| `_claude/workflows/CLAUDE.md` | される | 同上 |
| `scripts/CLAUDE.md` | **されない** | **5420 byte**。tmux 系スクリプトの不変条件の正本 |
| `CLAUDE.md` (repo root) | **されない** | プロジェクト規約 (テスト手順・禁止事項) |

## なぜ危険か

- **`scripts/CLAUDE.md` は load-bearing な不変条件を持つ**。「共有の観測ログに書くスクリプトは
  `tt_on_default_server` を通す」「`tmux_server_watchdog.sh` は `trap '' TERM` が生命線」など、
  破ると本番の死因分類が汚れる / watchdog が死ぬ類の規約。これが **1 台のマシンにしか無い**
- 2026-08-25 に issue 079 の作業でこのファイルへ不変条件を追記したが、**commit できていない**
  (作業した本人しか持っていない)
- **`_claude/rules/claude-md-layer-prompt.md` はレイヤーディレクトリへの CLAUDE.md 作成を
  推奨している**。ルールが作れと言うものを .gitignore が黙って捨てる、という噛み合わせの悪さ
- 新規 clone・別マシン・CI では存在しないため、そこで作業する人 (や Claude) は規約を知らずに踏む

## 対応案

1. **`.gitignore` の `CLAUDE.md` を絞る** — 意図が「`/init` が生成する root の CLAUDE.md を
   無視する」なら `/CLAUDE.md` (先頭スラッシュ = repo root 限定) にする。
   ただし root の中身も規約として価値がある (テスト手順・禁止事項) ので、追跡する判断もありうる
2. **`git add -f scripts/CLAUDE.md`** — 最小の応急処置。ただし .gitignore が残る限り、
   次に作られるレイヤー CLAUDE.md は同じ運命をたどる
3. 追跡しないと決めるなら**その理由を .gitignore にコメントで書く** (今は無言なので、
   次の人が「意図的なのか事故なのか」を判断できない)

**1 + 2 の併用を推奨**。ただし `.gitignore` はユーザーの領分なので判断を仰ぐこと。

## ⚠️ 着手前に確認すること

`CLAUDE.md` を ignore した意図が「機微な内容をコミットしない」である可能性がある。
その場合は中身を確認してから追跡すること (追跡は取り消しにくい)。

## 関連

- `_claude/rules/claude-md-layer-prompt.md` — レイヤー CLAUDE.md の作成を促すルール
- `_claude/rules/claude-md-maintenance.md` — 「CLAUDE.md は code の一部として扱う」(追跡前提の記述)
