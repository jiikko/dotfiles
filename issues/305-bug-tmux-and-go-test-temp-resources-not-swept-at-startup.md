# bug: テストが起こした隔離 socket / 一時ディレクトリが「中断で残る」まま誰も回収しない

起票日: 2026-09-06
カテゴリ: bug
優先度: 中（残骸は実測で溜まっている。実害は衛生と TMPDIR の圧迫）
出典: /audit resource-leaks 2026-09-06（forge Minimum+）

## 何が起きているか

テスト / probe の後始末が **`trap` / `defer` だけ**に預けられており、
**中断（Ctrl-C / `-timeout` の SIGQUIT / panic / 外部 kill）では走らない**。
残骸を回収する経路が無いので、一度残ると永久に残る。

### ① tmux 隔離 socket（6 本、最長 29 日）

`tests/tmux/test_mark_seen.sh` は `SOCK="dfms-$$"` + `trap cleanup EXIT` の形。
実測の残骸一覧と、既存 6 本の処遇（要承認）は **issue 304**。

`scripts/tmux_reap_orphan_servers.sh` は「socket が生きたまま放置されたテストサーバ」を
**意図的に対象外**にしており（同ファイル冒頭）、後片付けはテスト側の責務と書かれている。
**責務の所在は書かれているが、果たし方が実装されていない。**

### ② Go テストの一時ディレクトリ（40 個）

```
$TMPDIR/glogx-test-cache*  =  40 個（実測 2026-09-06）
```

`src/glogx/tui_helpers_test.go:TestMain` は `MkdirTemp` → `m.Run()` → `RemoveAll` の形で、
**`m.Run()` から戻らない終了では末尾の `RemoveAll` に到達しない**。
同じ形が他に 2 箇所ある:

- `src/doctor/disk/main_test.go`（`disk-delete-cache-`）
- `src/glogx/issues/main_test.go`（`glogx-issues-test-tmp`）

## 推奨対応

1. **`MkdirTemp` の直前に起動時の掃除を足す**。3 箇所から呼べるヘルパー 1 つにする
   （参照実装は `scripts/with_fresh_worktree.sh:sweep_stale` — 「自分の prefix かつ pid が
   生きていないもの」だけを消す形で、理由もコメントに書かれている）
2. tmux テストの共通 lib にも同じ起動時掃除を入れる
3. **socket 名の prefix を repo 共通の識別子へ揃える**。現状は `dfms-` / `rl` / `rs` / `t3-` /
   `__readonly_review_test_` とバラバラで、**掃除の母集合を機械で書けない**
4. `_claude/rules/tmux-probe-requires-socket-isolation.md` に
   「隔離した socket は**起動時掃除で回収する**」を 1 行足す（今は責務の所在で止まっている）
5. **検証は「掃除が実際に消した件数」**で見て、0 件を成功にしない
   （[`verify-execution-not-just-exit-code.md`](../_claude/rules/verify-execution-not-just-exit-code.md)）

## 🚨 掃除は破壊的操作の新設なので、次の 3 つを同じ commit で

（[`adversarial-review-own-safeguards.md`](../_claude/rules/adversarial-review-own-safeguards.md) /
[`sandbox-real-destructive-test-apis.md`](../_claude/rules/sandbox-real-destructive-test-apis.md)）

- **`os.Lstat` で symlink を skip**し、**`Uid == os.Getuid()`** を確認する。
  macOS の TMPDIR は per-user（`/var/folders/<...>/T`）なので現環境のリスクは低いが、
  **TMPDIR は env で差し替え可能でテストは日常的に差し替える**（`TMPDIR=/tmp` で走らせた
  瞬間に前提が変わる）
- 「pid が生きていない」判定は **pid 再利用の窓**を持つ（repo は `_tt_gc_stale_holds` で同じ問題を
  認識済み）。**`tmux -L <name> kill-server` に留める限り被害は自 socket に閉じる**が、
  生の `kill <pid>` へ一般化すると無関係のプロセスを殺せる。**この線引きをヘルパーのコメントに固定する**
- mtime ガードだけでは「長く走っているが最近その dir へ書いていない並行テスト」を守れない。
  各テストが自分の dir に pid を書き、掃除側は pid の生死で判定する

## 関連

- issue 304（既存 6 本の tmux 孤児をどうするか。ユーザー判断待ち）
- issue 299 / 308（同じ「trap だけに預けた後始末」ファミリーの shell 版）

## 進捗 (2026-09-08) — 未着手。325 から材料を引き継いだだけ

**この issue 自体はまだ着手していない。** issue 325（発生源の遮断）を片付けた副産物として、
「TMPDIR 隔離では原理的に消せない残骸」がこの issue の担当として確定し、調査の起点まで
分かったので転記する。着手するときはここから始められる。

### 325 からの引き継ぎ

issue 325（発生源の遮断）で層 1 を実装した結果、**TMPDIR 隔離では原理的に消せない残骸**が
この issue の担当として残った。325 の実測を前提として使えるので転記する。

🚨 **macOS の `mktemp` は TMPDIR 環境変数を見ない**（実測 2026-09-08。`mktemp -d` も
`mktemp -t pfx` も `_CS_DARWIN_USER_TEMP_DIR` を使い、従うのは `mktemp "$TMPDIR/x.XXXXXX"` の
ようにパスを自分で組む形だけ）。つまり **`Makefile` の runner が各テストへ配る使い捨て TMPDIR は、
テスト自身の素の `mktemp -d` には効かない**。後始末はテスト側の trap の責任のまま。

スイート 1 回で実機 `$TMPDIR` に残るのは **5 個**（14,147 個規模から減った）。
うち 3 個は repo 由来ではない（`TemporaryDirectory.*` / 中身は `.keep-directory`。
repo を grep して 0 件、8/25 から継続発生、今日だけで 45 個）。**この issue が拾うべきは残り 2 個**:

- `tests/zshrc/av1ify/test_av1ify_clipboard.sh` — `tests/zshrc/av1ify/test_helper.sh:12` の
  `TEST_TMP="$(mktemp -d)"` が残る。**`trap cleanup EXIT` があり、`cleanup` の先頭で
  `rm -rf "$TEST_TMP"` しているのに残る**（単独実行でも再現。`rc=1` で終わるので
  cleanup 自体は走っているように見える）。原因未特定 — ここが調査の起点
  - 🚨 `test_av1ify_clipboard.sh:20` が `TEST_TMP="${TEST_TMP:A}"` で symlink 解決した値へ
    書き換えている（`/var/...` → `/private/var/...`）。cleanup が見る値との関係を先に確かめる
- tmux 系（中身が `home` / `tmux` の `tmp.*`）— `isolate_env.sh` の `HOME="$TMUX_TMPDIR/home"` を
  含むので、`TMUX_TMPDIR=$(mktemp -d)` が消えずに残った形。①の socket 残骸と同じ「中断で trap が
  走らない」系の可能性が高い
