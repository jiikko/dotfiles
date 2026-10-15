# 777 (refactor): src のパッケージを超えた重複を寄せる (外部コマンドの起動口の検査・文字数で切る・設定の置き場・画面の記憶)

> 🚨 **担当中: dotfiles-63**（2026-10-16〜）

起票日: 2026-10-16

## 概要

ユーザー依頼 (2026-10-16): 「src の下も同じように繰り返しの実装を探して、パッケージを超えて共通化してください。fable を使っていいよ」。
773 (1 回目の調査。767〜772 で寄せた) の続き。fable のサブエージェント 1 体が 2 回目の洗い出しをし、main が本文で確かめた。
外部コマンドの起動口の検査は fable のサブエージェントが専用の worktree で実装し、main が検閲して取り込んだ。

## 母集合の数え方

src の 22 module。773 の観点に加え、非テストの .go を `exec.Command(Context)?\(`・`Getenv("XDG_*")|".config"`・JSON の load 関数・`syscall.Flock`・`^func .*(Age|Ago|Dur|human|Size)`・
`^func .*(clip|trunc|fit)`・`"~/"`・時刻の layout・`"tmux"` で、_test.go を `func TestMain`・`t.Setenv("HOME"|"PATH")`・待ちの helper の名前・偽のコマンドの書き出し・`type .*[cC]lock` で数えた。

## 寄せたもの

1. **外部コマンドの起動口の検査** (glogx・ratelimit・treefiler の 3 つの写し) → `src/subproc/boundarycheck` (`Run(t, Spec)`)。各 module のテストは数行で呼ぶ。
   pro-con と mac-lan-manager には検査が無く、**pro-con では pipe を持つのに WaitDelay の無い起動が 22 本** (調査時点の 13 本 + gitx.run の 2 本など) あった → subproc の口
   (`CommandContext` / 新設の `Command` / `GitCommand`) に寄せた。時間の上限は 1 つも足していない (WaitDelay だけ)。自分で `WaitDelay = time.Second` を張る 9 本は Allow (理由つき) で据え置き。
   mac-lan-manager の pbcopy は `subproc/clipboard.Copy` に (769 の mac-lan-manager 版)
2. **設定の置き場** (treefiler・mac-lan-manager の ledger / prefs・pro-con の 4 実装。XDG_CONFIG_HOME の扱いも割れていた) → 新しい共有 module `src/userdirs`
   (`ConfigBase` / `ConfigDir` / `ConfigFile` / `MoveIfMissing`)。**2026-10-16 のユーザー決定**: 道具は将来 glogx から分離するので `~/.config/glogx` の下に置かず道具ごと
   (`$XDG_CONFIG_HOME/<道具>`、未設定なら `~/.config/<道具>`)・XDG に従う・前の置き場のファイルは初回に自動で移す (新しい方にあれば上書きしない)。
   テスト用の差し替え (`TREEFILER_CONFIG_DIR` / `MAC_LAN_MANAGER_CONFIG_DIR`) は据え置き、差し替えているときは移さない。この Mac には `~/.config/glogx` が無かった (2026-10-16 に確認)
3. **「n 文字で切って … を足す」5 本** (pro-con の card.ClipRunes・live・store・dispatcher の clipLine と ratelimit) → `termsafe.ClipRunes`
4. **バイト数の上限で切る 2 本** (doctor/svc の cleanText・glogx の cutRunes) → `termsafe.CutBytes`。**どちらも上限を 1 rune 超えて返していた** (上限 10 に対し ASCII で 11 バイト) のを直した
5. **glogx の「閉じたときの画面の記憶」5 本** (issues / doctor / ratelimit / F / L) → glogx の中の `screenMemory[T]`。画面ごとの TTL の定数 (どれも同じ値の別名) も 1 つに
6. **pro-con のテストの「条件を待つ」helper 4 本** → `pro-con/internal/testwait` (`Until` / `Poll`)

## 寄せないもの (理由)

- **mac-lan-manager のテストの関門 (writeGuard) を safefs/sandbox へ**: safefs/sandbox はテストごとに登録し、拒否を記録してスイートを落とす作り。mac-lan-manager の台帳のテストは関門の拒否そのものを
  確かめるケースが 4 本あり、寄せると各ケースで記録を取り出す書き換えが要る。2 つの写し (ledger / prefs) の間に挙動の差は無い
- **テストの待ちの helper を module をまたいで** (runtimeout・proctree・zundamon・process_supervisor・lockman・treefiler・restartable): 新しい module と replace・8 本の workflow の paths の追加がかかり、除ける行は 1 本 10 行。
  restartable は go install する module でローカルの module を import できない
- **flock を試す 9 か所**: パーミッション・O_NOFOLLOW・所有者の検査が違い、restartable は import できない。除ける重複は各 5 行
- **`~` の展開 6 本・tmux の prefix の問い合わせ 2 本・経過時間の書式 7 本・pro-con の時刻の layout**: HOME が空のときの倒し方や返す表記が違うか、見た目の決定 (772 の続きにできる)
- **TestMain の一時 dir**: ratelimit は doctor を持つので `doctor/testtmp.Setup` を使える (issue 305 の規律の取りこぼし) が、ほかは doctor を持たない
- **却下 (773 のとおり)**: プロセスグループの停止 5 実装・ruleguard の写し・人が読むサイズ・ps の解析・pid の生死・docker / ssd の 10 進・キャッシュ 3 つ・restartable 全般・
  pro-con の git の 5 本 (gitx.Run の返し方に収まらない理由が各所にある)・doctor/runner.Exec (Setpgid と SIGKILL を自分で持つ)・fake clock・zundamon の extcmd / outfile

## 残り

- [x] mac-lan-manager の main.go の usage と README の置き場の文言を新しい置き場に直した (747 の push 6c41bbdd の後)
- `cmd.WaitDelay = time.Second` の 9 本は subproc.WaitDelay (2 秒) と値が割れたまま (寄せるなら別の issue)
- process_supervisor は見張りの子に WaitDelay を張らない (supervisor.Spec.Command を書く側も同じ穴を持ちうる。未確認)

## 進捗

- [x] 実装 (commit: 「refactor(glogx): 全画面の「閉じたときの画面の記憶」5 本…」/「fix(termsafe): バイト数の上限で切る処理を CutBytes に…」/「test(pro-con): テストの「条件を待つ」helper 4 本…」/
  「subproc: 外部コマンドの起動口の検査を boundarycheck に寄せる」/「pro-con / mac-lan-manager: 外部コマンドの起動を subproc に寄せ…」/「subproc/boundarycheck: go list との突き合わせを…」/
  「refactor(termsafe): 「n 文字で切って … を足す」5 本…」/「refactor: 設定の置き場を userdirs に寄せ…」)
- [x] 変異 (mutate-verify。全部 red): 画面の記憶の未来の時刻の判定を外す → 3 画面のテスト / CutBytes を前の実装に戻す → TestCutBytes / userdirs の上書きの守りを外す → TestMoveIfMissing /
  前の置き場から移すのを外す → TestConfigFileMovesLegacy / 起動口の検査 (fable): pro-con の 1 本を素の exec に戻す・glogx の注記を消す・glogx に素の起動を足す・ratelimit に os/exec を足す → それぞれの検査、
  検査器の別名検出・go list との突き合わせ・代入の判定を壊す → fixture と canary
- [x] 敵対的レビュー (opus・read-only): **P1**: glogx の go.mod に replace userdirs を足したのに own_sources_test の ownSourceExcluded に足しておらず、
  TestOwnSourceRootsCoverLocalReplaces が落ちた (CI の src/glogx が赤になる。glogx はテストの一部しか回していなかった) → 足した /
  P2: 相対パスの XDG_CONFIG_HOME を受け付けると前の置き場の設定を cwd の下へ移した → XDG の仕様どおり無視 /
  P3: 前の置き場が相対 symlink だと移した後に壊れる → 絶対パスの link を作り直す / 移せなかったら前の置き場を返して読み続ける / 起動口の検査が見ない形 (os.StartProcess・syscall.ForkExec) を doc に /
  testwait.Until は前の waitUntil (20ms × 500 回。負荷で 10 秒を超えて待つ) より負荷時の待ちが短い (壁時計で 10 秒) → 記録のみ。
  壊せなかった: 両方の置き場にあるときの扱い・2 プロセスの同時の引っ越し・sync.Once と差し替えの env・本物の ~/.config を触る経路・pro-con の 22 本の置き換えの意味・Allow の理由・
  screenMemory / CutBytes / ClipRunes の等価・CI の paths と replace (check_go_project_lanes rc=0)
- [x] make test (776 と 777 を合わせた木): go-lint・go-test は restartable の 1 本 (触っていない。単独で 3 回 rc=0。負荷の下の不安定さ) を除き rc=0
