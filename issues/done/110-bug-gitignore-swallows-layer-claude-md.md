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

## 🚨 着手前に確認すること

`CLAUDE.md` を ignore した意図が「機微な内容をコミットしない」である可能性がある。
その場合は中身を確認してから追跡すること (追跡は取り消しにくい)。

## 関連

- `_claude/rules/claude-md-layer-prompt.md` — レイヤー CLAUDE.md の作成を促すルール
- `_claude/rules/claude-md-maintenance.md` — 「CLAUDE.md は code の一部として扱う」(追跡前提の記述)

---

## 対応 (2026-08-28): 着手前の検証で **既に解決済み**と判明。コード変更なしで done

着手時に「issue の記述を鵜呑みにしない」で実コード・git 履歴に照らしたところ、本文の
前提 (`.gitignore:12` に無条件の `CLAUDE.md` がある) が既に成り立っていなかった。

### 何が起きていたか

| issue の記述 | 現在 (2026-08-28 実測) |
|---|---|
| `.gitignore:12` に無条件の `CLAUDE.md` | **行そのものが無い** (`d9f8e62` で削除) |
| `scripts/CLAUDE.md` が追跡されない (5420 byte) | **追跡済み** (`d9f8e62` で追加。現在 5953 byte) |
| repo root の `CLAUDE.md` が追跡されない | **追跡済み** (`e2262d2` で追加) |

つまり **対応案 1 + 2 の併用**(推奨案どおり) が `d9f8e62`「全階層の CLAUDE.md を無視する行を
外し、scripts/CLAUDE.md を追跡する」で実施済みだった。issue の起票 (2026-08-26) 後に
別セッションが対応したが、issue を done へ移していなかったため open のまま残っていた。

### 内容が欠けていないことの確認

追跡は「その時点の版」を固定するので、**欠けた版が commit された可能性**を別に確かめた:

- `d9f8e62` 時点の `scripts/CLAUDE.md` は **5420 byte** = issue が記録したサイズと一致
  (追跡前に中身が失われてはいない)
- issue が名指しした load-bearing な不変条件は両方とも現存:
  `tt_on_default_server` を通す規約 / `tmux_server_watchdog.sh` の `trap '' TERM`
- 全 CLAUDE.md に未コミットの差分なし

### 再発しないことの確認 (この issue の本質)

本質は「**今後作るレイヤー CLAUDE.md が黙って飲まれない**」こと。パターン単位でなく
`git check-ignore` で判定した (global gitignore も評価されるため):

- 未作成のレイヤーパス 5 つ (`src/schedkeys/` `zshlib/` `bin/` `_claude/hooks/` `tests/tmux/`)
  はいずれも ignore されない
- `~/.gitignore_global` に `CLAUDE.md` / `*.md` 系のパターンなし (`*.swp` `*~` `*tmp-browserify*`
  `**/.claude/settings.local.json` のみ)
- repo の `.gitignore` に md を飲む行なし

### 残るリスク (対応せず。次の audit が再生成しないよう理由を残す)

**ignore ではなく「作ったまま `git add` を忘れる」経路は塞いでいない。** `scripts/CLAUDE.md` が
長期間 1 台にしか無かった直接の原因は ignore だが、ignore を外した今でも「新しいレイヤー
CLAUDE.md を作って commit し忘れる」は起こりうる。ディスク上の CLAUDE.md が全て追跡されて
いるかを検査するゲート (`tests/claude/test_claude_links_complete.sh` と同型) で塞げるが、
**この issue の範囲外**なので実施していない。必要になったら別 issue で起こす。
