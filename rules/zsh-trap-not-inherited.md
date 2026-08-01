# zsh の `trap '' SIG` は、サブシェルとバックグラウンドジョブには継承されない

## ルール

- **`trap '' HUP TERM INT` でシグナルを無視させたシェルの下で、守りたいコマンドを `( ... )` のサブシェルや `&` のバックグラウンドジョブに入れない**。zsh はどちらにも trap をリセットするので、そこで ignore が失われる
- ignore が届くのは **trap を張ったシェル自身が exec する foreground のコマンドだけ**。`cd` したいなら、サブシェルを掘らずコマンド側のオプションで済ませる (`go build -C dir` / `make -C dir` / `git -C dir` / `tar -C dir`)
- **bash では同じコードが動く**ので、bash の常識で書くと踏む。POSIX は「ignore に設定されたシグナルは fork/exec を越えて継承される」と定めており、bash はそのとおりに振る舞うが、**zsh は subshell / async job で trap を落とす**

## 実測 (zsh 5.9, macOS。victim へ直接 SIGHUP を撃つ)

| 形 | ignore が届くか |
|---|---|
| `( trap '' HUP; victim )` — trap を張ったシェルの foreground | ✅ 届く |
| `( trap '' HUP; ( victim ) )` — **サブシェルを 1 枚挟む** | ❌ 届かない |
| `( trap '' HUP; victim & wait )` — **バックグラウンドジョブ** | ❌ 届かない |
| `( victim )` — trap なし (対照) | ❌ 届かない |
| bash で `( trap '' HUP; ( victim ) )` | ✅ 届く (zsh との差) |

## なぜ (起源: dotfiles `bin/lib/go_autobuild.zsh`, 2026-08-01)

tmux popup から起動した glogx が、裏で走らせた `go build` を **exit 129 (=128+SIGHUP)** で失う
不具合が出た。popup を閉じるとプロセスグループへ HUP が飛ぶためで、`_go_autobuild_spawn` は
まさにそれを見越して `trap '' HUP TERM INT` を張っていた。にもかかわらず死んでいたのは、
ビルド本体が `(cd "$src_dir" && go build ...)` と**サブシェルの中**にあり、そこで ignore が
落ちていたから。`go build -C "$src_dir"` にしてサブシェルを外すだけで直った。

厄介なのは症状の残り方で、失敗すると `.autobuild.failed` が残り、TTL (10分) が切れるまで
再ビルドしない。ユーザーからは **「古い版で動いています と出るのにビルドされない」** という、
シグナルとは結びつかない形で見えた。原因に辿り着けたのは
`build failed (exit $rc)` が exit code をログに残していたからで、これが無ければ追えなかった。

## 気づきにくさ

- **静的解析で捕まらない**。shellcheck は zsh を解析しない (SC1071)。`zsh -n` は構文しか見ない
- **普段は動く**。シグナルが飛んでこない限り正しく動くので、テストも普通は緑になる
- **書いた本人の意図は正しい**。「trap を張ったのだから守られているはず」という前提が、
  bash と同じだと思っている限り疑われない

## やること / やらないこと

- ✓ trap で守る範囲は「そのシェルが直接 exec する foreground コマンド」に収める
- ✓ ディレクトリを変えたいだけなら `-C` 系のオプションを使う (fork を作らない)
- ✓ シグナルで死にうる子プロセスは exit code をログに残す (`exit 129` = 128+SIGHUP が唯一の手がかりになる)
- ✓ 「シグナルを浴びせて生き残るか」の回帰テストを書く (実行しないと分からない種類の欠陥なので、
  レビューでは防げない)
- ✗ `trap ''` の下で `( ... )` / `&` を挟む
- ✗ bash で動いたから zsh でも動く、と考える

## 例外

- `nohup` は exec の境界で HUP を SIG_IGN に立て直すので、どの層で ignore を失っても効く。ただし
  **stdout が tty のとき cwd に `nohup.out` を作る**ため、リポジトリ内で走らせる用途には向かない
  (この不具合でも一度は採用しかけて、副作用を理由に `-C` へ切り替えた)
- 守る対象が本質的にバックグラウンドジョブなら、trap ではなくジョブ側で disposition を立て直すしかない

## 関連

- `bin/lib/go_autobuild.zsh` — 実装と、同じ罠の行内再発防止コメント
- `tests/bin/test_go_autobuild.sh` — 「spawn したビルドは HUP で死なない」の回帰テスト
- [`zsh-hook-return-via-reply.md`](zsh-hook-return-via-reply.md) — 同じ「zsh 固有の挙動を知らないと踏む」系
- `~/.claude/rules/pending-issue-rationale-in-code.md` — 実装で強制できない制約はコード直近に残す
