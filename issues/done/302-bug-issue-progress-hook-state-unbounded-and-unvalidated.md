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
  （[`adversarial-review-own-safeguards.md`](../../_claude/rules/adversarial-review-own-safeguards.md) 節 2）

→ **fail-closed** にすること（既定パスと一致するか、hook 自身の marker を含むことを**実行前に**検査し、
外れたら実行せず失敗させる）。

## 検証

- 掃除: 古い mtime の `.head` / `.reported` と**対象外の拡張子**を同じ dir に置き、後者が残ることを見る。
  判定は「消した件数」
- 検証: `"session_id": "../x"` を含む JSON を流し、**state_dir の外にファイルが生まれないこと**を見る
  （exit code で判定しない）

## 結果（2026-09-09）

### 🚨 増加は予測どおりだった（実測で裏を取ってから着手した）

| 時点 | ファイル数 |
|---|---|
| 起票日 2026-09-06（hook を入れた当日） | **1** |
| 2026-09-09 | **174**（696K） |

**3 日で 174**。issue が書いた「1 プロジェクトあたり 1 日約 100 セッション」の見積もりと整合する。
バイト数は小さいが、効くのは inode / ディレクトリエントリの単調増加、という読みも変わらない。

### ① TTL 掃除（SessionStart 側）

`issue_progress_sweep` を新設。**保持は既定 14 日**（`CLAUDE_ISSUE_PROGRESS_TTL_DAYS` で上書き可、
数字と桁数を検証）。推奨対応の「採ってはいけない実装」を全部避けた:

| 却下されていた形 | 実装 |
|---|---|
| `-type f -delete` で広く消す | **`-name '*.head' -o -name '*.reported'` で必ず絞る**（state_dir は env で差し替わる） |
| `2>/dev/null \|\| true` で握り潰す | **1 件ずつ rc を見て、消せなかった件数を stderr に出す** |
| 件数を数えない | **掃除した件数を返し、0 でなければ stderr に出す**（0 件を成功の証拠にしない） |

掃除の失敗で hook 自体は止めない（基準点の記録の方が主目的）。

### ② session_id の検証

`issue_progress_valid_session_id`（`[!0-9A-Za-z-]` の allowlist）を lib に置き、**両 hook** から呼ぶ。
`.reported` を書く check 側も同じ検証を通る。

### 🚨 テストのバグを 1 件踏んだ

`hook()` ヘルパーが **`2>/dev/null` で stderr を捨てて**いたため、掃除の件数（stderr 側の観測点）を
拾えず「掃除が動いていない」と一度誤診した。stderr を取る `hook_err()` を足して解決。
**観測点を stderr に置いたなら、テストのヘルパーがそれを拾えるかを先に確かめる**、という形。

### 変異検証（4 本、すべて red / baseline green）

| 変異 | 落ちた assert |
|---|---|
| 掃除を消す（`swept=0`） | 掃除した件数を報告する / 古い `.head` が残っている |
| 拡張子の絞りを外す | **対象外の `keep.txt` を消した** |
| `-maxdepth 1` を外す | **サブディレクトリまで潜って消した** |
| session_id の検証を外す | 不正な session_id で状態ディレクトリの外へ書いた |

集約経路 `make test-dir DIR=tests/claude` に `[run] tests/claude/test_issue_progress_check.sh`。
`make test-lint` rc=0。

### 残タスク

- **検出しないと決めた形**: `.head` / `.reported` 以外の拡張子で状態を持ち始めたら、
  掃除の対象からも外れる（そのときは `issue_progress_sweep` の `-name` を同じ変更で足す）
- **未実測**: 掃除が実環境で 174 件を実際に消すところ（テストは使い捨て dir で 2 件を消す形で固定）。
  次のセッション開始時に stderr へ件数が出るので、そこで観測できる
