# bug: issue-progress hook の状態ファイルが無限に増え、session_id を検証せずパスに使っている

起票日: 2026-09-06
カテゴリ: bug
優先度: 中（増加は「コード上確定」で実測はまだ 1 ファイル — 導入当日のため。実測ペースは下記）
出典: /audit resource-leaks 2026-09-06（forge Minimum+）。2 エージェントが独立に検出 + クロスレビューが 1 件追加

対象: `_claude/hooks/issue-progress-start.sh` / `_claude/hooks/issue-progress-check.sh` /
`_claude/hooks/lib/issue-hooks.sh:issue_progress_json_field`

## ① 状態ファイルに prune / TTL / 上限が一切無い

SessionStart が `<state_dir>/<session_id>.head` を、Stop が `<session_id>.reported` を書くが、
**消す経路がどこにも無い**。

```
$ grep -rn 'mtime\|prune\|TTL\|MAX_' _claude/hooks/issue-progress-*.sh _claude/hooks/lib/issue-hooks.sh
（0 件）
```

周辺の類似機構はすべて上限を持っている（`TT_TRIGGER_LOG_MAX_LINES=5000` /
`TMUX_SCHEDULE_KEYS_LOG_MAX_LINES=2000` / `TT_PSLOG_KEEP`）。**新設だけがこの規律から外れている。**

### 実測（2026-09-06）

- 状態ディレクトリ: **1 ファイル / 62 byte**（hook を入れた当日なので、これは「まだ増えていない」だけ）
- 増加ペースの裏取り: `~/.claude/projects/-Users-koji-dotfiles/*.jsonl` は
  **1024 件（8/4〜9/6 の約 33 日）**、**直近 7 日で 676 件**。
  この 1 プロジェクトだけで **1 日あたり約 100 セッション**
- 状態ディレクトリは**全プロジェクト共通**（session_id だけをキーにする）なので、実際の増加はこれより速い
- バイト数は小さい（60 byte × 年 36,000 ≒ 2MB）ので、効くのは **inode / ディレクトリエントリの単調増加**

## ② session_id を検証せずパス構成要素に使っている

```sh
session_id=$(issue_progress_json_field "$input" session_id)
[ -n "$session_id" ] || exit 0
...
printf '%s\n%s\n' ... >"$state_dir/$session_id.head"     # 検証なし
```

`issue_progress_json_field` は jq 経路では**任意の文字列**を返し、jq が無い環境の sed 経路
（`issue-hooks.sh:125`）は `"` `,` `}` だけを除外して **`/` と `..` を通す**。

供給元が Claude Code なので実害の確度は低いが、**同じ commit で塞ぐべき理由が 2 つある**:

- hardening が `case` 文 1 行で済む
- **①の TTL 掃除を入れると「書く場所」と「消す場所」が同時にずれる**（掃除が状態ディレクトリの
  外を消しにいく経路ができる）

## 推奨対応

1. id 取得直後に **`case "$session_id" in *[!0-9A-Za-z-]*|'') exit 0 ;; esac`** を両 hook へ
2. SessionStart 側に TTL 掃除を置く（SessionEnd はクラッシュ・強制終了で走らないので単独では閉じない）
3. 🚨 **削除対象を `-name '*.head' -o -name '*.reported'` で必ず絞る**
4. 保持期間は env で上書き可能にし、テストが差し替えて「**掃除が実際に消した件数**」を見る
   （0 件を成功にしない）
5. 同じ commit で hook 冒頭の「状態:」行に保持期間を記す

## 🚨 採ってはいけない実装（却下理由）

```sh
find "$state_dir" -maxdepth 1 -type f -mtime +7 -delete 2>/dev/null || true   # ✗
```

- **削除の根が上書き可能な env**（`CLAUDE_ISSUE_PROGRESS_DIR`。テストが実際に差し替えている:
  `tests/claude/test_issue_progress_check.sh:19`）
- SessionStart で**毎回無条件に走る**
- **`2>/dev/null || true` が失敗も握り潰す**ので、壊れても観測できない
  （[`adversarial-review-own-safeguards.md`](../_claude/rules/adversarial-review-own-safeguards.md) 節 2）

→ **fail-closed** にすること（既定パスと一致するか、hook 自身の marker を含むことを**実行前に**検査し、
外れたら実行せず失敗させる）。

## 検証

- 掃除: 古い mtime の `.head` / `.reported` と**対象外の拡張子**を同じ dir に置き、後者が残ることを見る。
  判定は「消した件数」
- 検証: `"session_id": "../x"` を含む JSON を流し、**state_dir の外にファイルが生まれないこと**を見る
  （exit code で判定しない）
