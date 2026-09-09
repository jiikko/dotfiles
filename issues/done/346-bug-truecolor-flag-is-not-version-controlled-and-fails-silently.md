# bug: 色が出るかを決める `SUPPORT_TRUECOLOR` が version 管理外で、外れると無音で色が壊れる

起票日: 2026-09-09
カテゴリ: bug
優先度: 中（**壊れたときに黙る**形。エラーは出ず「なんか色が変」になるだけ）
出典: [retro 342](342-retro-solargraph-spin-loop-and-ruby-lsp-migration-2026-08-31.md) の残課題 ①

対象: [`_nviminit.lua`](../../_nviminit.lua) の `dotfiles_truecolor_supported`（54 行付近）/ `~/.zshenv`

## 何が起きるか

`_nviminit.lua` は truecolor 非対応の端末で colorscheme を `retrobox` + `termguicolors=off` へ
落とす。判定の優先順は:

1. `SUPPORT_TRUECOLOR=false/0` ← **シェルが export する「唯一信頼できる」信号**
2. `SUPPORT_TRUECOLOR=true/1`
3. `COLORTERM=truecolor/24bit` → 対応 / `TERM_PROGRAM=Apple_Terminal` → 非対応（best-effort）
4. 不明 → **対応とみなす**（gruvbox 維持）

このマシンでは 1 が効いている（実測 2026-09-09: `SUPPORT_TRUECOLOR=false` /
`termguicolors=false` / `colorscheme=retrobox`）。

🚨 **その 1 行は `~/.zshenv` にあり、dotfiles の管理外**（ファイル自身のヘッダが
「このファイルは dotfiles 管理外。マシンごとに置く」と宣言している）。

## 🚨 フォールバックが効かないことを実測した

「1 行が無くても 3 が拾うのでは」は**成り立たない**:

```
$ nvim --headless -u _nviminit.lua ... print(vim.env.TERM_PROGRAM)
TERM_PROGRAM= nil        # ← tmux の中では渡ってこない
```

`_nviminit.lua:40` のコメントも「`_tmux.conf` が `,tmux*:RGB` で RGB を無条件広告し、
COLORTERM は伝わらず、**TERM_PROGRAM も**」と書いている。つまり tmux 内では 3 が発火せず、
**4 に落ちて「対応」と誤判定**する。

→ `.zshenv` の 1 行が消えた瞬間、tmux 内の nvim は gruvbox + termguicolors=on になり、
**256 色端末で色が潰れる**。エラーは出ない。

## なぜ単純に dotfiles へ取り込めないか

`SUPPORT_TRUECOLOR` は**マシンごとに違う**（この端末は非対応、別マシンは対応かもしれない）。
`_zshenv` として commit すると、対応マシンでも retrobox に落ちる。
retro 342 の「dotfiles へ `_zshenv` として取り込むか」は、そのままでは採れない。

## 対応案

1. **`setup.sh` が `~/.zshenv` の不在／該当行の不在を検出して警告する**（作らない。人に決めさせる）。
   判定できるのは「行が無い」ことだけで、対応／非対応は人しか知らない
2. **`bin/dotfiles-check` 系に足す**（起動のたびに気づける）
3. **`_zshenv.example` を dotfiles に置き、`setup.sh` が「無ければコピーを促す」**
   （中身はコメントアウトした 1 行 + 判定の説明）
4. 何もしない。代わりに `_nviminit.lua` の判定 4 を「不明 → 非対応とみなす」へ**反転**する
   ← 🚨 これは**新しい端末を壊す**方へ倒すので、`list-masked-failure-modes-before-removing-guard.md`
   の手順（何がマスクされるかの列挙）を踏んでから

**1 か 3 が安い**。4 は影響範囲が逆向きなので単独では採らない。

## 受け入れ条件

- [x] `~/.zshenv` に該当行が無い状態を作って、**気づける**ことを実測で示す
- [x] マシンごとに違う値であることが、仕組みの中に書かれている（`_zshenv.example` のコメント等）
- [x] **変異検証**: 検出をやめると red になる

## 進捗: 対応案 2 + 3 を採った (commit `fix(346): SUPPORT_TRUECOLOR の設定漏れを起動時に検出する`)

**採らなかった案と理由**: 案 1（`setup.sh` が警告）は「setup.sh を再実行したときしか出ない」ので、
`.zshenv` を後から失った場合に気づけない。案 4（判定を「不明 → 非対応」へ反転）は
新しい端末を壊す向きなので単独では採らない（本文の判断どおり）。

- **案 2**: `_zshrc` に `_dotfiles_check_truecolor` を足した（`_dotfiles_check_claude_links` の隣。
  状態ファイルを持たず fork ゼロ、対話シェルの起動ごとに評価する）。
  検出は 2 つ: **未設定 / 空** と、**`_nviminit.lua` が解釈しない値**（`yes` 等）。
  後者は「設定したのに効かない」形で、ファイルを見ているだけでは分からない
- **案 3**: `_zshenv.example` を置いた（`setup.sh` の link 対象は明示リストなので**自動では張られない**
  ＝ 人が `cp` して選ぶ、が保たれる）。「なぜ dotfiles が値を持てないか」「tmux 内で自動判定が
  効かない理由」を本文に書いた

### 「気づける」ことの実測 (pty で実際の対話 zsh を起動した A-B)

`script(1)` の pty で `zsh -i`（`HOME` / `ZDOTDIR` は使い捨て、`dotfiles` は symlink）:

| 条件 | 出力 |
|---|---|
| `SUPPORT_TRUECOLOR` 未設定 | `[dotfiles] SUPPORT_TRUECOLOR が未設定です。… 'cp ~/dotfiles/_zshenv.example ~/.zshenv' して true/false を選んでね。` |
| `SUPPORT_TRUECOLOR=false` | 該当行 **0 件**（黙る） |

関数単体ではなく**実シェルの起動経路**（`[[ -o interactive ]] && [[ -t 1 ]]` の内側）で出ることを見た。

### 2 実装の食い違いを機械で止める

受け付ける値は `_zshrc` と `_nviminit.lua` の**2 箇所**にある（片方だけ増やすと
「設定したのに効かない」/「効いているのに警告」が黙って起きる）。
`tests/zshrc/test_truecolor_flag_check.sh` が**両方から値集合を抽出して突き合わせる**
（`0 1 false true`）。抽出が空なら失敗にする。

### 変異検証 (5 本すべて red)

| 変異 | 結果 |
|---|---|
| 呼び出し `_dotfiles_check_truecolor` を消す（定義だけ残す） | RED（配線の pin） |
| 未設定の腕を黙らせる | RED |
| 解釈されない値の腕を黙らせる | RED |
| `_zshrc` だけ `yes` を受け付けるよう広げる | RED（lua との突き合わせ） |
| `_zshenv.example` を消す | RED |

🚨 最初の 2 本は sed の当て方が悪く**構文エラー**になっていた。`zsh -n` で「変異がビルド不能」を
第 3 の結果として弾いてから当て直した（緑でも赤でもないものを red と読まない）。

### 走った証拠

`make test-dir DIR=tests/zshrc` rc=0。ログに
`[run] tests/zshrc/test_truecolor_flag_check.sh` → `[test-truecolor-flag-check] すべて成功` が出ている
（集約経路から実行されていることの確認）。

### 残タスク

- **このマシンは既に `~/.zshenv` に行があるので、警告は出ない**（実測: `SUPPORT_TRUECOLOR=false`）。
  新しいマシンをセットアップしたときに初めて実運用で発火する

## 関連

- [retro 342](342-retro-solargraph-spin-loop-and-ruby-lsp-migration-2026-08-31.md) — 出典
- `_nviminit.lua:38-52` — 判定の優先順とその理由
