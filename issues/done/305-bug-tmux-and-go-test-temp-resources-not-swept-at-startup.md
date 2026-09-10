# bug: テストが起こした隔離 socket / 一時ディレクトリが「中断で残る」まま誰も回収しない

> 🚨 **担当中: dotfiles-4b**（2026-09-10〜）。`issues/next/` に claim 済み。
> `adversarial-review-own-safeguards.md` §7 の敵対レビューを 3 周まで回して P1 を都度修正しており、
> **4 周目を残して作業中**です。横から done へ送らないでください。
> 触っているファイル: `tests/tmux/test_socket_cleanup.sh` / `test_reap_orphan_servers.sh` /
> `tests/tmux/lib/reap_mktemp.sh` / `tests/tmux/lib/kill_socket.sh`

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
   （[`verify-execution-not-just-exit-code.md`](../../_claude/rules/verify-execution-not-just-exit-code.md)）

## 🚨 掃除は破壊的操作の新設なので、次の 3 つを同じ commit で

（[`adversarial-review-own-safeguards.md`](../../_claude/rules/adversarial-review-own-safeguards.md) /
[`sandbox-real-destructive-test-apis.md`](../../_claude/rules/sandbox-real-destructive-test-apis.md)）

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

## ① を実装した（2026-09-10）— ただし真因は「起動時掃除が要る」ではなかった

### 🚨 決定的な実測: **`tmux kill-server` は socket ファイルを消さない**

```
tmux -L probe -f /dev/null new-session -d 'sleep 30'
tmux -L probe kill-server
→ /private/tmp/tmux-501/probe が残る     ← SIGKILL で殺した場合も同じ
```

つまり **中断時だけでなく、正常終了のたびに 1 個ずつ漏れていた**。
535 個が溜まった説明はこれで付く（`ctrlv-test-*` 約 150 / `pane-state-bell-*` 約 100）。

**この issue の前提が変わる**。起票時は「trap が中断で走らないから残る」＝
**掃除機構（起動時に走査して消す）が要る**、という読みだったが、実際は
**正常系で漏れている**ので、§0-A の「そもそも発生させない構造」が取れる。

### 発生源を断った（掃除機構は作っていない）

`tests/tmux/lib/kill_socket.sh` の `tt_tmux_kill_socket <-L の名前>`:

1. **kill する前に** `display -p '#{socket_path}'` で実パスを控える
   （殺してからでは取れず、「判定不能だから消さない」に倒れて残骸になる）
2. `kill-server`
3. **控えたそのパスだけ**を消す

**走査しないので、母集合の取り違えで他人のものを消す経路が原理的に無い**
（本番の `default` を除外するロジックすら本来は不要。二重の保険として名前で弾いてはいる）。

発生源 2 本（`test_ctrl_v_paste.sh` / `test_tmux_pane_state_bell.sh`）を差し替え、
**実行前後で既定 socket dir が 1 個（`default`）のまま**であることを実測した。

### 新設した検査 `tests/tmux/test_socket_cleanup.sh`（6 件）

🚨 **canary を先に置いた**: 「`kill-server` は socket を残す」を検査自身が毎回確かめる。
tmux 側が将来消すようになったら、この検査は何も守らなくなるので、そのとき気づけるようにした。

配線の pin も入れた（ヘルパー単体の検査だけでは「呼び出し側が使っている」を 1 mm も守らない）。

### 変異検証（4 本、すべて red / baseline green）

| 変異 | 落ちた assert |
|---|---|
| `rm` をやめる | socket ファイルを残した / 死んでいるサーバの回収も落ちる |
| 死後のパス組み立てを消す | 死んでいるサーバの socket を回収できていない |
| `default` 保護を外す | 🚨 default を消した |
| 呼び出し側を `kill-server` だけに戻す | `test_ctrl_v_paste.sh` が socket を置き去りにする形へ戻っている |

### 残タスク

- [ ] **中断（SIGKILL）で trap が走らない場合**は今も残る。名前に `$$` が入るので次の run が
      同じ名前を作らず、自動回収されない。**検出しないと決めた形**として検査のヘッダに明記した
      （回収手順は上の「掃除を実行した」節に実測つきで残してある）
- [ ] **空になった一時ディレクトリ 39 個**（`/private/tmp/reapp.*` など）は未処理。
      これは `mktemp -d` の殻で、socket とは別の発生源（`TMUX_TMPDIR` を作るテスト側の trap）
- [ ] 他に `-L` を使うテストが増えたら同じヘルパーを使う。現状 `LINT_DIRS` の外なので
      機械では強制していない（検査は既知の 2 本を pin するだけ）

## 空の一時ディレクトリ 39 個も発生源を断った（2026-09-10）

残タスクに残っていた `/private/tmp/reapp.*` の正体は **`test_reap_orphan_servers.sh` の
cleanup 漏れ**だった。掃除機構は作っていない（§0-A）。

### 内訳を数え直した

```
$ ls -d /private/tmp/reap* | sed -E 's|.*/(reap[a-z]*)\..*|\1|' | sort | uniq -c
   1 reapl
  39 reapp
   1 reaps
```

`reapl` / `reaps` は cleanup の `rm -rf` に載っているので 1 個ずつしかない（= 中断で trap が
走らなかった 1 run 分）。**39 個の `reapp` だけが桁違い**で、これはケース E の
`PROT_DIR=$(mktemp -d /tmp/reapp.XXXXXX)`（`test_reap_orphan_servers.sh:258`）が
`cleanup()` の `rm -rf` に**入っていなかった**ため。中断ではなく **正常終了のたびに 1 個**漏れる。

`reap_log_probe_dir` も同型の穴を持っていた（成功パスでは inline の `rm -rf` で消えるが、
その手前で `fail` すると trap に載っていないので残る）ので、まとめて cleanup へ寄せた。

### 直した形

```sh
rm -rf "$ORPHAN_DIR" "$LIVE_DIR" "$SPACE_BASE" "$ATT_DIR" \
  ${PROT_DIR:+"$PROT_DIR"} ${reap_log_probe_dir:+"$reap_log_probe_dir"}
```

### 検査を足した（`test_socket_cleanup.sh` の ⑥、7 件）

`mktemp -d` した変数がすべて cleanup の **`rm -rf` の行**に載っていることを pin する。
🚨 **cleanup 全体を見る形は false green になる** — `kill-server` の
`TMUX_TMPDIR="$LIVE_DIR"` に当たってしまい、`rm -rf` から外しても緑になる。実装では
`rm -rf` の行（継続行を含む）だけを抽出している。抽出が 0 件になったら緑にせず落とす canary つき。
件数のハードコード（`検査 6 件`）も、検査を足すたびに腐るので `checks` カウンタへ変えた。

### 実測（A-B）

| | 実行前 | 実行後 |
|---|---|---|
| 修正後 | 41 個 | **41 個**（増えない） |
| 変異（`PROT_DIR` を `rm -rf` から外す） | 41 個 | **42 個**（1 個ずつ漏れる） |

`make test-lint` rc=0 / `test_reap_orphan_servers.sh` rc=0 / `test_socket_cleanup.sh` 13 件 fail=0。

### 変異検証（3 本、すべて red / baseline green）

| 変異 | 結果 |
|---|---|
| `PROT_DIR` を `rm -rf` から外す | ✗ `$PROT_DIR` が cleanup の rm -rf に無い |
| `cleanup()` を `do_cleanup()` に改名（抽出を壊す） | ✗ canary が発火（検査 7 件で打ち切り） |
| `LIVE_DIR` を `rm -rf` からだけ外す（`kill-server` 行には残す） | ✗ false green にならず red |

### 残タスク

- [ ] **既存の 41 個の殻**（`/private/tmp/reap*`。中身は空の `tmux-501/` のみ）は未削除。
      発生源は断ったので今後は増えない。削除はユーザー承認待ち
- [ ] SIGKILL で trap が走らない場合は今も残る（上節と同じ「検出しないと決めた形」）

## 敵対レビュー (opus) が検査 ⑥ に P1 を 2 件出した — 直して変異で確認した

`adversarial-review-own-safeguards.md` の最終ゲート。**「壊す手順を見つけろ」で投げた結果、
1 周目の検査 ⑥ は 5 通りで素通りできた**（実 mirror で走らせて確認された）。
「変異 3 本 red」は自分が想定した形しか試していなかった、の実例。

### P1-1: 抽出正規表現が **repo の主流イディオムを見ていなかった**

`^[A-Za-z_][A-Za-z0-9_]*=\$\(mktemp -d` は「行頭・クォートなし」だけを拾う。実測（`tests/` + `scripts/`）:

| 書き方 | 件数 |
|---|---|
| `VAR="$(mktemp -d` (クォート形) | **58** |
| `VAR=$(mktemp -d` (素の形) | 59 |
| インデント付き | 12 |

`test_reap_orphan_servers.sh` の現行 6 個がたまたま素の形なだけで、
**次に dir を足す人が house style で書いた瞬間に無言で通る**。

### P1-2: cleanup 内の **コメントが本物の `rm -rf` として通っていた**

`inside && /rm -rf/` はコメント行も拾う。しかも**この変更自身が確率を上げていた** —
`rm -rf` の 3 行上に「🚨 後から作る一時 dir もここへ足す」という指示コメントを置いたので、
将来の編集者が「`rm -rf "$FOO"` のように足すこと」と書くのは自然で、それが検査を満たす。

### P2-1: ⑥ が **tmux プローブの `exit 77` の下**にあった

⑥ は純粋なテキスト照合で tmux に依存しないのに、①（`tmux new-session`）が失敗すると
全体が skip になっていた（実測: 偽 tmux を PATH 先頭に置くと ⑥ が 1 件も走らず rc=77）。

### 直した形

- 抽出を `^[[:space:]]*VAR="?$(mktemp[[:space:]]+-d` に広げ、**コメント行を落とす**
- 照合を部分一致から**識別子境界つき**へ（`$PROT` が `$PROT_DIR` に当たっていた）
- **⑥ を ① より前へ移動**。加えて「tmux は起動できないが ⑥ が違反を出している」なら
  skip (77) に畳まず rc=1 にする
- canary を**同じ抽出関数に既知の入力を通す**形へ作り替えた（クォート形 / インデント /
  継続行 / コメント除外を 1 つの fixture で固定）。旧 canary の `vars < 5` は
  「抽出が壊れたか」ではなく「dir が何個あるか」を測っており、**dir を減らす正しい
  リファクタで red になる**閾値だった
- `checks -ge 13` を assert（「1 件も走らないまま 0 件 fail=0 で緑」を塞ぐ）

### 変異検証（レビューが突破した 4 形 + canary、すべて red / baseline green）

| 変異 | 1 周目 | 修正後 |
|---|---|---|
| インデント付きの新 dir を cleanup に足さない | 緑（素通り） | **red** |
| クォート形 `VAR="$(mktemp -d …)"` の新 dir | 緑（素通り） | **red** |
| cleanup に注意書きコメント + `rm -rf` から外す | 緑（素通り） | **red** |
| 既存変数の接頭辞になる名前（`PROT` vs `$PROT_DIR`） | 緑（素通り） | **red** |
| 抽出を旧正規表現へ戻す | — | **red**（canary が発火） |
| 偽 tmux + 違反あり | 77 (skip) | **rc=1** |

`make test-lint` rc=0 / `test_socket_cleanup.sh` 14 件 fail=0 /
`test_reap_orphan_servers.sh` rc=0・残骸 41→41 / `test_runner_isolates_tmpdir.sh` 12 件通過。

### 🚨 判別力の本体は静的検査ではない（脅威モデルをヘッダに明記した）

5 つの突破口はすべて「dir をどう綴ったか」に依存していた。綴りに影響されない観測は
**実行前後の `/tmp/reap*` の個数差**（A-B）だけで、静的検査は「素の書き忘れを早く止める
事前フィルタ」。`adversarial-review-own-safeguards.md` §8 に従い、**検出しないと決めた形**
（`rm -fr` の綴り違い / 変数経由の削除 / heredoc 内の `mktemp -d`）をヘッダへ書いた。

### 残タスク

- [ ] **既存の 41 個の殻**（`/private/tmp/reap*`。中身は空の `tmux-501/`）は未削除。
      発生源は断ったので増えない。削除はユーザー承認待ち
- [ ] `test_reap_orphan_servers.sh` は 4 つの `mktemp -d` を **`trap cleanup EXIT` の
      インストール前**に作っている（28–32 行 vs 72 行）。この窓で `set -e` が発火すると
      4 個まとめて漏れる。今回の変更とは無関係の既存の形なので触っていない
      （実測の `reapl` / `reaps` 各 1 個はこの窓か SIGKILL で説明が付く）
- [ ] SIGKILL で trap が走らない場合は今も残る（「検出しないと決めた形」として明記済み）

## 一般則の転記: fixture を `mktemp -d` で作ると「論理パス vs 物理パス」を必ず踏む

dotfiles-a8 が issue 347 で踏んだ形（2026-09-10）。この issue の担当範囲ではないが、
**一時 fixture を作るテスト全般に効く**ので転記する。

macOS の `mktemp -d` は `/var/folders/…` を返し、`/var` は `/private/var` への symlink。
そのため **同じディレクトリに 2 つの表記**ができる:

| 取り方 | 結果 |
|---|---|
| 引数をそのまま使う / `pwd`（論理） | `/var/folders/…/T/x` |
| `pwd -P` / `find` の出力 / `grep -r` の出力 | `/private/var/folders/…/T/x` |

**パス同士を `=` / `!=` / prefix 剥がしで比べている箇所があると、静かに外れる**
（347 では `[ "$other" != "$dest" ]` が常に真になり、移動した当人が二度処理されていた。
別のガード 1 枚のおかげで無害だったが、そのガードに依存していた）。

**この issue で触った tmux テスト側には該当が無いことを確認済み**（2026-09-10 実測）:

- `test_socket_cleanup.sh` / `test_reap_orphan_servers.sh` / `lib/kill_socket.sh` に
  **パス同士の比較は 0 件**
- `ROOT_DIR` は `pwd`（論理）だが、**読み込みパスの組み立てにしか使っていない**
- `kill_socket.sh` の socket パスは `rm -f` の**対象**であって比較対象ではなく、
  `default` 除外も `${path##*/}`（basename）なので表記の差を受けない

判定の目安: **一時 dir のパスを「消す対象」に使うだけなら安全、「同一性の判定」に使うなら
`pwd -P` で片側へ揃える**。

## §7 の 2 周目: 検査 ⑥ は **6 通りで素通り**した → 軸を構文から外した

1 周目の修正で新設した機構を攻め口として渡した 2 周目（opus）が、**実 mirror で 6 通りの
false green を再現**した。「変異 3 本 red」で通した実装が、である。

### P1-A: 宣言した 4 つの修正のうち「識別子境界」だけが**何にも守られていなかった**

`adversarial-review-own-safeguards.md` §1.5（段ごとに変異を当てる）の違反。

| 変異 | 結果 |
|---|---|
| 抽出を旧正規表現へ戻す | red（canary 発火）✅ |
| awk のコメント除外を消す | red（canary 発火）✅ |
| `rm_covers` を境界なしの部分一致へ戻す | **緑のまま通る** ❌ |

🚨 **commit message の主張が誤りだった。** 「既存変数の接頭辞になる名前（`PROT` vs
`$PROT_DIR`）→ red」と書いたが、その red は**変異体にしか存在しない fixture**（`PROT` を足した
状態）に依存していた。**同じ commit が `PROT_DIR` の被覆を直した結果、HEAD には接頭辞衝突する
変数が 0 件**（実測: 6 変数の総当たりでペア 0）。つまり「境界照合は変異で確認済み」は
**変異体については真、HEAD については偽**だった。

### P1-B: `rm_covers` が「rm の引数」でなく「行のテキスト」を見ていた

1 周目 P1-2（コメントが本物の `rm -rf` として通る）と**同じクラス**。コメント*行*を落としても、
**行内コメント**は残る:

```sh
    ${PROT_DIR:+"$PROT_DIR"} …   # $NEWD_DIR はケース X 側で個別に消している
```
これで `✓ $NEWD_DIR は cleanup の rm -rf に載っている` になり、**正常終了パスで 1 件漏れる**
ことを最小再現で実証された。`… || print -u2 "残った: $NEWD_DIR"` でも同じ。

### P2-C: 抽出が 1 つの綴りしか見ていない（1 周目 P1-1 と同じクラス）

`typeset X="$(mktemp -d …)"` / `export X="$(mktemp -d …)"` / 1 行に 2 つ / `mktemp -q -d`
の 4 形がいずれも緑（抽出件数が 6 のまま増えない）。**`export` 前置きはこの repo に実在する**
（実測 2 箇所）。

### 🚨 レビュワーの結論を採った: **6 回目の正規表現拡張はしない**（§8 の stopping rule）

「迂回を直すたびに新しい迂回が出るなら、それは gate の穴ではなく**規則の軸が構文にある印**」。
**発生源側の構造を変えた**:

```zsh
typeset -ga REAP_TMPDIRS=()
reap_mktemp_d() {          # <変数名> <テンプレート>
  local d; d=$(mktemp -d "$2") || return 1
  REAP_TMPDIRS+=("$d")
  typeset -g "$1"="$d"
}
# cleanup:
  if (( ${#REAP_TMPDIRS} )); then rm -rf "${REAP_TMPDIRS[@]}"; fi
```

**作成と登録が 1 つの関数**なので、「作ったが `rm -rf` に書き忘れる」が**構造的に起きない**。
検査 ⑥ は「素の `mktemp` がヘルパーの外に無いか」という**1 イディオム**を見るだけになり、
綴りの揺れ（P2-C の 4 形）にも行内コメント（P1-B）にも影響されない。`rm_covers`（P1-A の
無検査だった防御）は**消えた**。

### 🚨 最初この形を壊して書いた

`X=$(reap_mktemp_d …)` の **stdout 返し**で書いたが、コマンド置換はサブシェルなので
**配列への登録が親シェルへ届かず、cleanup が 1 件も消さないまま緑**になる形だった。
`typeset -g` で呼び出し元の変数へ入れる形に直し、**「stdout へ返す形に戻す」変異も pin した**。

### そのほか直したもの

- **P3-D**: `checks -ge 13` が **dir の個数に結合**していた（旧 canary の `vars < 5` を捨てた
  理由と同じ結合を 1 段上で再導入していた）。dir を減らす正しいリファクタが閾値割れで red に
  なる。⑥ が dir 数に依存しなくなったので固定値 11 にした
- **P3-F**: この検査自身に **trap が 1 つも無く**、SIGTERM 60 回で canary の一時ファイルが
  7 件残った（一時ファイルの後始末を守る検査が自分で漏らす自己矛盾）。`trap … EXIT INT TERM HUP`
  を足した
- 🚨 **自分で pipefail の罠を再生産した**。ヘルパーの pin を `awk … | grep -qE` で書き、
  `scripts/check_pipefail_grep_q.sh` に落とされた（pipefail 下では grep -q が先に抜けて awk が
  SIGPIPE で死に、**一致しているのに rc≠0** になって判定が反転する）。`$( )` で退避してから
  `<<<` で渡す形に直した

### 変異検証（9 本すべて red / baseline green）

素の `mktemp` を足す / `typeset` + `-q -d` + インデント / `export` 前置き / 1 行に 2 つ /
行内コメントで「消している」と主張しつつ漏らす / ヘルパーが stdout で返す形へ戻す /
登録をやめる / cleanup が消さない / ヘルパーを改名 / 語境界を外す（canary 発火）。

**A-B（判別力の本体）**: `test_reap_orphan_servers.sh` rc=0・`/tmp/reap*` は **41 → 41**。
canary の一時ファイル残骸 **0 件**。

## 🚨 2026-09-10: **検証しているテスト自身が、同じ resource を 48 個漏らしていた**

別セッション（dotfiles-a8）が残骸を数え直して発見。**両セッションが独立に同じ数を測って一致**した。

### 実測

```
/private/tmp/tmux-501/  = 51 エントリ / lsof で生きているのは default の 1 個だけ
  tt-cleanup-b 20 / tt-cleanup-a 18 / tt-cleanup-raw 10   ← 48 個が test_socket_cleanup.sh 由来
  pane-state-bell 1 / ctrlv-test 1                        ← b81fa2e0 (01:24) より前の残骸
作成時刻: Sep 10 10:05:54〜10:06:00 (= 発生源修正のずっと後)
走っているテストプロセス: なし (最新の残骸から 240 秒経過)
48 ÷ 3 (1 回の実行で raw / a / b の 3 個) = 16 回 ≒ 3 周目で回した変異の回数
```

### 原因

`test_socket_cleanup.sh` は socket を**成功パスでしか消していない**（① `rm -f -- "$raw_path"` は
`ok` の腕、②③ の `rm -f -- "$p1"` / `"$p2"` は `bad` の腕）。**79 行の trap は `$canary_src` しか
消さない**ので、**途中で落ちた実行では作った socket がそのまま残る**。
**変異検証は「わざと落とす」実行**なので、当てるたびに 3 個ずつ漏れた。

さらに、このテストは `TMUX_TMPDIR` を隔離していない（`unset TMUX TMUX_PANE` だけ）。
既定の socket dir を使うのは「`default` を消さないこと」を検査するうえで意図的だが、
そのぶん**漏れ先が本番の socket dir 直下**になる。

### なぜこれが重いか

**この issue の failure mode（後始末を trap に預けて中断では走らない）が、その修正を
検証しているテストの中で再生産されていた。** しかも:

- 2 周目の敵対レビューは **P3-F として canary ファイル 1 件だけ**を指摘しており、
  **socket は見ていなかった**（実害は 1 桁大きかった）
- 判別力の根拠にしていた **A-B（`/private/tmp/reap*` が 41 → 41）は `tmux-501/` の socket を
  1 つも数えていない**ので、この漏れは**構造的に見えない**。
  「増えないことを確認した」は、**測っていない resource については何も言っていなかった**

`verify-execution-not-just-exit-code.md`「その機構を外したら観測結果は変わるか」の対偶
（**観測していないものは、機構の有無に関わらず同じ結果になる**）の実例。

### 直し方（合意済み。3 周目のレビューが返った直後に入れる）

`reap_mktemp_d` と同じ「作成と登録を 1 関数へ寄せる」形を socket にも当て、
`trap cleanup_all EXIT INT TERM HUP` で**独立に**消す。

🚨 **後始末に `tt_tmux_kill_socket`（検査対象そのもの）を使わない**のが要点。
使うと、それを壊す変異を当てたときに後始末も一緒に壊れて漏れる（今回まさにその状況）。
`default` は名前で除外する（本番は絶対に触らない）。

### いま触っていない理由

`tests/tmux/` は §7 の 3 周目レビューが読んでいる範囲。ここで書き換えると
`parallel-write-agents-need-worktree-isolation.md` の「read-only のレビュー中に同じファイルを
書き換えない」に反し、**実在しないコードを前提にした P1** が返る形を自分で作る。
漏れているのは 0 バイトのファイル 48 個で、増えるのは変異を当てたときだけなので待つ方が安い。

### 残骸の由来（削除の承認を求めるときはこの区分で）

| 対象 | 由来 | 状態 |
|---|---|---|
| `/private/tmp/reap*` の dir **41 個** | 修正前の `test_reap_orphan_servers.sh`（発生源は `b81fa2e0` / `10a0fef8` で断った） | 中身は空 |
| `tt-cleanup-*` **48 個** | **2026-09-10 の変異検証**（発生源はこれから直す） | 0 バイト |
| `pane-state-bell` / `ctrlv-test` 各 1 個 | `b81fa2e0`（01:24）より前の run | 0 バイト |

`default` は 1 個で無傷、本番 tmux は 30 セッションのまま。

## 🚨 2026-09-10: 「残骸の削除」に承認は要らなかった — **`/private/tmp` は再起動で消える**

ユーザーの指摘（「残骸が残っている件だが、OS 再起動で消えるでしょ？」）を実測で確かめた。**そのとおり**。

```
最後の起動: 2026-07-04 18:52:08  (uptime 67 日)
/private/tmp のエントリ: 580 件
  そのうち **起動より古いもの: 0 件**        ← 1 件も生き残っていない
/etc/periodic/daily/110.clean-tmps: **無し**  ← 定期掃除の仕掛けは無い
/etc/periodic.conf: 無し (既定のまま)
```

580 件すべてが起動後に作られている＝**起動時に一掃されている**（定期掃除は無いので、
掃除しているのは boot だけ）。よって:

| 対象 | 置き場 | 再起動で消えるか |
|---|---|---|
| `/private/tmp/reap*` の dir 41 個 | `/private/tmp` | ✅ **消える** |
| `tt-cleanup-*` 48 個 | `/private/tmp/tmux-501/` | ✅ **消える** |
| `pane-state-bell` / `ctrlv-test` 2 個 | 同上 | ✅ **消える** |
| （参考）349 の `*.pre-dein-vim` 4 本 | **`~/.config/nvim`** | ❌ **消えない**（HOME） |

### 何が変わるか

- **「既存の 41 個の殻を消す」は残タスクから落とす。** 承認を取って手で消す価値が無い
  （0 バイトの殻で、次の再起動で消える）。**304 のときに 537 個の削除で承認を取ったのも、
  厳密には不要だった**可能性が高い（あちらはプロセスの kill が主目的だったので判断は別）
- **発生源の修正（trap）は依然として要る。** 再起動が掃除するのは「そのとき在るもの」で、
  **同じ boot セッション中は増え続ける**（今日 1 回の 3 周目で 48 個増えた）。
  溜まると測定のノイズになり、実際に今日の A-B の読みを歪めた
- **この issue の判断軸を「溜まっているか」から「増加が止まる機構があるか」へ**。
  それは 308 が既に書いていた基準（「今いくつあるか」ではなく「増加が止まる機構があるか」で見る）で、
  今回はその基準を自分で適用し損ねていた

### 🚨 一般則として

**「残骸が溜まっている」を報告する前に、その置き場が誰にいつ掃除されるかを確かめる。**
`/private/tmp`（boot で一掃）/ `/private/var/folders`（dirhelper が定期掃除）/ `HOME`（誰も掃除しない）
で扱いがまったく違う。今回はそれを確かめずに**削除の承認を求めていた**。

## 締め (2026-09-11) — ① を全数勘定つきで閉じる

前セッションが **95bddea8 を本文へ書かずに離脱した**ので、その記録・実測・全数勘定をここに置く。
以降のセッションはこの節だけ読めばよい（上の各節の残タスクは末尾の決着表を見る）。

### 95bddea8「検査自身の socket 漏れを塞ぎ、ヘルパーを lib へ出してアンカーを無くす」

直前の節（「検証しているテスト自身が 48 個漏らしていた」）で **合意済みだった直し方が実装済み**。
`tests/tmux/test_socket_cleanup.sh` はファイル全体を 1 本の `trap tt_cleanup_all EXIT INT TERM HUP` で
覆い、socket は `tt_new_socket` が**名前を決めた瞬間に登録**する。**後始末に検査対象そのもの
（`tt_tmux_kill_socket`）を使わない**ので、それを壊す変異を当てても後始末は壊れない。
併せて 3 周目 P1-2（`}` の行末コメント 1 つで信頼窓が 6 → 51 行へ無警告で広がる）に対応して、
ヘルパーを `tests/tmux/lib/reap_mktemp.sh` へ出し、**検査から awk も行範囲もアンカーも消した**。

### A-B 実測（2026-09-11）— 3 母集合を同時に数えた

🚨 前回の A-B が `/tmp/reap*` しか数えず **socket 97 個を構造的に見落とした**ので、今回は
`tmux-501/`・`reap*`・`/private/tmp` 全体を同時に数えた（§6「その計測が構造的に見落とすもの」）。

| | tmux-501/ | reap* | /private/tmp 全体 |
|---|---|---|---|
| 実行前 | 100 | 29 | 390 |
| `make test-tmux` (rc=0) の後 | 100 | 29 | 390 |
| + `test_tmux_pane_state_bell.sh` + `test_debounced_save.sh` (rc=0) の後 | **100** | **29** | **390** |

直近 5 分に作られた socket は **0 件**。

**中断経路（この issue の本来の failure mode）も測った**。socket が生まれた瞬間に `kill -TERM`
を撃つ（生まれる前に撃つと「機構の有無で結果が変わらない観測」になる）:

| | rc | この run が残した socket |
|---|---|---|
| 現行 | 1 / 143 | **0** |
| 変異（`trap tt_cleanup_all …` の行を消す） | 143 | **1**（`before=99 → after=100`） |

= この A-B には判別力がある（機構を外すと観測結果が変わる）。変異は当てる前に `bash -n` で
構文を確認し、1 世代の `cp` から復元して `git diff` が空であることを見た。

### `-L` を使う 13 箇所の全数勘定 — **追加の漏れ元は 0 件**

残タスクに「他に `-L` を使うテストが増えたら同じヘルパーを使う」とあったので数え直した。
**判定軸は「socket が共有 dir に落ちるか」**であって、ヘルパーを呼んでいるかではない:

| 経路 | socket の落ち先 | 後始末 | 対象か |
|---|---|---|---|
| `tests/tmux/test_ctrl_v_paste.sh` / `tests/claude/test_tmux_pane_state_bell.sh` | **共有**（既定の socket dir） | `tt_tmux_kill_socket` | ✅ 対応済み（`b81fa2e0`） |
| `tests/tmux/test_socket_cleanup.sh` | **共有**（`default` を消さないことを検査するので意図的） | 独立した `tt_cleanup_all` | ✅ 対応済み（`95bddea8`） |
| `test_mark_seen.sh` / `test_fork_scratch.sh` / `test_tmux.sh` / `bench_tmux.sh` / `test_smooth_scroll.sh` | 使い捨て `TMUX_TMPDIR` の中 | `rm -rf "$TMUX_TMPDIR"` で **dir ごと** | ❌ 対象外（socket も一緒に消える） |
| `tests/claude/test_deny_bare_tmux_kill.sh` | — | — | ❌ hook へ渡す**文字列**で、tmux を起こさない |
| `tests/zshrc/tmux-session/test_debounced_save.sh` / `scripts/tmux_resurrect_debounced_save.sh` / `scripts/check_syntax.zsh` | — | — | ❌ socket パスは stub の照合値・構文検査の対象 |

静的な読みだけでは閉じない（この issue の履歴は「正しく見えるのに走らない cleanup」ばかり）ので、
上の A-B が経験的な裏取りになっている（5 本の `TMUX_TMPDIR` 隔離勢も含めて通しで走らせて増えなかった）。

### 推奨対応 4 を規範化した（この issue から外へ出る唯一の一般知）

`_claude/rules/tmux-probe-requires-socket-isolation.md` に 2 項足した。起票時の文面
（「隔離した socket は**起動時掃除で回収する**」）は**採らない** — 設計が変わったため:

- **`tmux kill-server` は socket ファイルを消さない**ので、kill する前に `#{socket_path}` を控え、
  kill 後にそのパスだけを `rm` する（正本は `tests/tmux/lib/kill_socket.sh`）
- **走査して消す掃除機構は作らない**（母集合を取り違えた瞬間に本番の socket へ届く）

### 過去の残タスクの決着表（上の各節の `- [ ]` はこの表で置き換わる）

| 節 | 残タスク | 決着 |
|---|---|---|
| 推奨対応 3 / ①の初期案 | socket 名の prefix を repo 共通へ揃える | **不要**（`lsof` で prefix に依存せず書けると判明。さらに掃除機構自体が不要になった） |
| ① 再設計 | 起動時掃除（走査して消す）を `isolate_env.sh` へ | **不採用**（発生源を断つ側へ倒した。§0-A） |
| 掃除の実行 | 535 / 41 / 48 個の残骸を消す承認 | **不要**（`/private/tmp` は起動時に一掃される。実測 2026-09-10） |
| 3 周目 | `test_reap_orphan_servers.sh` が trap の**前**に `mktemp -d` する窓 | **解消済み**（現行は cleanup と trap が 50〜68 行、dir 作成は 72 行以降） |
| 推奨対応 4 | rule への 1 行 | **完了**（上記。文面は起票時と変えた） |
| ② | Go テストの一時 dir | **完了**（`src/doctor/testtmp`。変異 7 本 red） |
| 全節 | SIGKILL で trap が走らない場合 | **検出しないと決めた形**（`test_socket_cleanup.sh` のヘッダに脅威モデルとして明記済み。`/private/tmp` は起動時に一掃されるので溜まり続けない） |

残っている 100 / 29 個は**すべて修正前（〜2026-09-10 10:15）の残骸**で、次の再起動で消える。
