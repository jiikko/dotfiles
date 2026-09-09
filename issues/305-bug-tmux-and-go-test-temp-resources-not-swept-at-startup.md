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

## 進捗 (2026-09-09) — ② Go テストの一時ディレクトリを実装。① tmux socket は残作業

### 🚨 着手前に数え直した: ② の残骸は現時点で **0 件**

`glogx-test-cache*` / `disk-delete-cache-*` / `glogx-issues-test-tmp*` はいずれも 0 件。
issue 325 の runner 隔離（各スイートに使い捨て TMPDIR を配る）が効いて、`make test` 経由では
残らなくなっていた。

**それでも漏れる経路は実在する**ことを実測で確定させた（数字が 0 でも機構が要る根拠）:

```
$ go test -c -o /tmp/glogx.test ./ && /tmp/glogx.test -test.run Test & sleep 2; kill -9 $!
$ find $TMPDIR -maxdepth 1 -name 'glogx-test-cache*' | wc -l
1                      # ← SIGKILL は m.Run() から戻らないので末尾の RemoveAll に到達しない
```

### §0-A「作らずに済む構造はないか」— 掃除の母集合を自分の 1 dir に閉じた

TMPDIR 全体を prefix で走査する形（issue の推奨 1 の素朴な読み方）は**採らなかった**。
TMPDIR は env で差し替え可能でテストは日常的に差し替えるので、`TMPDIR=/tmp` で走らせた瞬間に
「他人のものを prefix で拾う」形になる。代わりに:

- 一時 dir は **`$TMPDIR/dotfiles-go-test/<prefix>.<pid>.<連番>`** に作る（`0o700`）
- 掃除が見るのは **その親 dir の直下だけ**。そこは自分が作ったもの以外入らない
- 「同じ dir 名を使い回して残骸を 1 件に抑える」案は**却下**。並行実行が同じ dir を共有して
  cross-talk する（325 の runner 経由なら TMPDIR が別なので衝突しないが、`go test` 直叩きを
  2 つ並べると衝突する）

### 新設: `src/doctor/testtmp`

`doctor` は `glogx` から replace で取り込まれているので、**新しいモジュールを足さずに 3 箇所から
呼べる**（`glogx` / `glogx/issues` / `doctor/disk`）。テスト専用ヘルパーのために CI レーンつきの
モジュールを 1 本増やすのは重いと判断した。

安全側の作り:

| 条件 | 実装 |
|---|---|
| root の**直下だけ**（再帰しない） | `os.ReadDir` 1 段 |
| **symlink を辿らない** | `os.Lstat` + `ModeSymlink` を skip |
| **自分の uid が所有** | `ownedByMe`（純関数へ切り出してテスト可能にした） |
| pid が**自分でなく、生きていない** | `syscall.Kill(pid, 0)` の errno |
| **プロセスは絶対に kill しない** | pid は「使用中か」の判定にのみ使う。消すのは dir だけ |
| 判定不能は**消さない側** | uid が読めない / pid が読めない / errno が ESRCH 以外 |
| 回収件数を返す | `Setup` の第 2 戻り値。呼び出し元が stderr へ報告（0 件を成功の証拠にしない） |

### 自分で踏んだバグ 2 件（どちらも変異が炙り出した）

1. **`alive()` が `os.FindProcess` + `Process.Signal` の文言判定だった**。reap 済みの pid に
   `os: process already finished` が返り、**死んでいるものを「生きている」と読む**（= 掃除が
   1 件も動かない）。`syscall.Kill` の errno（ESRCH / EPERM）へ変えた
2. **テストヘルパーの `t.Skipf` が変異判定を汚染していた**。`alive()` を壊す変異を当てると
   前提チェックが skip して緑になる。wait4 直後の pid 再利用は実質ゼロなので `t.Fatalf` へ

### 変異検証（7 本、すべて red / baseline green・3 回連続）

生死判定を無効化 / symlink を辿る / prefix 検証をやめる / swept を常に 0 / uid 検証を無効化 /
errno を取り違える（ESRCH → EPERM）/ `Sys()` 判定不能を通す。
ビルド不能になった変異は red / green に丸めず**第 3 の結果**として当て直した（実際 3 回起きた）。

### E2E の証拠

SIGKILL で `glogx-cache.87701.3279549249` が残った状態から `go test` を 1 回走らせると、
`dotfiles-go-test/` が空になることを確認。

### 既存の canary に 1 度落とされた

`doctor/disk` の `TestDestructiveCallsGoThroughHook` が
「`TestMain` が走査に入っていない（検査の対象が壊れている）」で赤くなった。一時 dir の後始末が
`testtmp` の cleanup へ移り、**この file 内に破壊的呼び出しを持たなくなった**ため。
「必ず見つかるはずの関数」の一覧から `TestMain` を外し、**理由と「走査自体は今も通る」ことを
コメントに固定**した（破壊的呼び出しを足せば今も報告される）。

## 残タスク — ① tmux 隔離 socket は未着手

**このセッションでは ② だけを閉じた。** ① を分けたのは、性質が違うため:

- ② が消すのは**ディレクトリ**だが、① が回収したいのは**孤児 tmux サーバ (プロセス)**。
  dir を消してもサーバは残る（`tmux-probe-requires-socket-isolation.md` の既知の罠）
- 消す = `tmux -L <name> kill-server` なので、**母集合を取り違えると本番のサーバを殺す**。
  実際 2026-07-30 に本番サーバ誤殺の事故が起きている
- 既存 6 本の処遇は **issue 304 でユーザー判断待ち**。判断が出る前に自動回収を入れると、
  その 6 本を勝手に消すことになる

着手するときに要るもの（issue の推奨 2・3・4 に対応）:

- [ ] socket 名の prefix を repo 共通の識別子へ揃える（現状 `dfms-` / `rl` / `rs` / `t3-` /
      `__readonly_review_test_` とバラバラで、**掃除の母集合を機械で書けない**）
- [ ] 揃えた prefix を母集合に、`tests/tmux/lib/isolate_env.sh` へ起動時掃除を入れる
- [ ] `_claude/rules/tmux-probe-requires-socket-isolation.md` に
      「隔離した socket は起動時掃除で回収する」を 1 行足す
- [ ] issue 304 の判断が出てから（勝手に既存 6 本を消さない）


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

## 🚨 ① の「溜まった証拠」が出た（2026-09-10 実測）

issue 304 の承認を受けて孤児サーバ 6 本を回収した際に、**別クラスの残骸**が見つかった。

```
$ ls -1 /private/tmp/tmux-501 | wc -l
536
$ lsof -U -a -u 501 -F n | sed -n 's/^n//p' | grep '^/private/tmp/tmux-501/'
/private/tmp/tmux-501/default          # ← 1 個だけ。これが本番
```

**536 個のうち生きているのは `default`（本番）1 個だけで、残り 535 個は死んだ socket ファイル**。
最古は **2026-07-05**（約 2 か月）。内訳は `ctrlv-test-*` / `pane-state-bell-*` /
`i206s-*` / `ctrlv-*` など、**テストが `-L <name>-$$` で起こしたもの**が大半。

### プロセスの掃除とはリスクが違う

| | 孤児サーバ（プロセス） | 死んだ socket ファイル |
|---|---|---|
| 消す対象 | **プロセス** | **0 バイトのファイル** |
| 取り違えたときの被害 | **本番サーバを殺す**（2026-07-30 に事故の実績） | 生きている socket を消す → tmux が繋がらなくなる（サーバは生きているので再作成で復旧しうる） |
| 母集合の判定 | argv / socket / 接続 fd を全部見る必要がある | **`lsof` が開いていないこと**の 1 条件 |

後者は**プロセスを触らない**ぶん構造的に安全で、しかも `default` を除外すれば
本番への経路が無い。①の「socket prefix を揃えないと母集合を機械で書けない」という
前提も、**「lsof が開いていない socket ファイル」なら prefix に依存せず書ける**。

### 残タスク（① の再設計）

- [ ] **prefix を揃える案は不要かもしれない**。母集合を「`$TMUX_TMPDIR`/`/private/tmp/tmux-$uid`
      配下の socket で、lsof が開いていないもの」にすれば prefix に依存しない
- [ ] ただし **`default` は必ず除外**する（`tmux_reap_orphan_servers.sh` が
      `protect_socks` でやっているのと同じ形。seam は「置換」でなく「追加」にする）
- [ ] 🚨 **`TMUX_TMPDIR` が既定と違う socket が実在する**（304 の実測: `reapl.*` / `reaps.*` /
      `/var/folders/.../tmp.*`）。`/private/tmp/tmux-$uid` だけを見ると取りこぼす
- [ ] 535 個の実掃除は**ユーザーの承認待ち**（304 の承認範囲はサーバ 6 本だった）

## 掃除を実行した（2026-09-10、ユーザー承認あり）

「死んだ socket ファイルの削除をお願いします」の承認を受けて実行。**プロセスは 1 つも触っていない**
（消したのは 0 バイトの socket ファイルだけ）。

### 母集合の作り方（そのまま自動化の仕様にできる）

```
対象 = その dir 直下の socket で、
       ① 名前が default でない          ← 🚨 本番を必ず除外
       ② lsof がどのプロセスからも開いていない
```

`lsof -U -a -u <uid> -F n` で「生きている unix socket」を先に一覧化し、その集合に**入っていない**
ものだけを対象にした。**prefix には一切依存していない**（`ctrlv-test-*` / `pane-state-bell-*` /
`i206s-*` などバラバラのままで問題なく書けた）。

🚨 **エントリ単位で plan → exec した**。一覧を作ってから一括削除するのではなく、
1 件ごとに「socket か / default でないか / lsof が開いていないか」を**消す直前に**取り直している
（検査から実行までの窓を 1 件ぶんに縮める。`sandbox-real-destructive-test-apis.md`）。

### 結果

| 場所 | 削除 | 失敗 | 直前判定でスキップ |
|---|---|---|---|
| `/private/tmp/tmux-501/`（既定） | **535** | 0 | 0 |
| 既定の外（`reapl.*` / `reaps.*`） | **2** | 0 | 0 |

- `/private/tmp/tmux-501/` は **`default` の 1 個だけ**になった
- **本番は無傷**: `tmux ls` = 30 セッション、`tmux_server_watchdog.sh` も稼働継続

### ① の設計が確定した（prefix を揃える案は不要）

起票時は「socket 名の prefix がバラバラで**掃除の母集合を機械で書けない**」が ① の前提だったが、
**その前提は誤りだった**。`lsof` を使えば prefix に依存せず書ける。推奨対応 3
（prefix を repo 共通の識別子へ揃える）は**不要**。

### 残タスク

- [ ] **テスト側の起動時掃除を実装する**（上の母集合の作り方をそのまま使う）。
      置き場は `tests/tmux/lib/isolate_env.sh`（tmux テストの共通隔離）
- [ ] 🚨 **既定の socket dir だけを見ないこと**。テストは `TMUX_TMPDIR` を差し替えるので、
      `/private/tmp/tmux-$uid` の外にも socket ができる（今回 2 件がそこにあった）
- [ ] **空になった一時ディレクトリ 39 個**（`/private/tmp/reapp.*` など）は**未処理**。
      socket は消えたが `mktemp -d` の殻が残っている。中身は空で害は小さいが、
      同じテストが作り続けるなら発生源側（`isolate_env.sh` の trap）で断つのが筋
