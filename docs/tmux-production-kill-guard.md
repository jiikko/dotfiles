# 本番 tmux サーバの誤殺ガード — なぜもう kill されないか

**読む trigger**: `bin/tmux` / `_claude/hooks/deny-bare-tmux-kill.sh` を触る / 「本番 tmux が消えた」が再発した / 隔離テストサーバの kill が想定外に拒否された。

コードの What は `bin/tmux` と hook が出典。ここに置くのは**なぜこの構造にしたか**と**何を守れて何を守れないか (残存リスク)**。

## 何が起きたか (2 度の実害)

| 日付 | 経路 | 被害 |
|---|---|---|
| 2026-07-30 | 別セッションの probe 後片付けが素の `tmux kill-server` を撃った (`$TMUX` が `TMUX_TMPDIR` に優先し本番を直撃) | 29 セッション消滅 |
| 2026-09-11 | 別セッションのサブエージェントが**テストスクリプト内部**で `-L default` への kill を撃った | 30 セッション消滅 |

2 度目の経路 (実測で確定):

```
存在しない TMPDIR を渡す → スクリプト内の mktemp -d が失敗 → guard_dir="" →
TMUX_TMPDIR="" で tmux を呼ぶ → tmux は空/不在の TMUX_TMPDIR を /tmp へフォールバック →
/tmp/tmux-501/default = 本番 socket を直撃
```

## なぜ既存の防御では止まらなかったか

2026-07-30 の後に **PreToolUse hook** (`deny-bare-tmux-kill.sh`) を入れ、Claude の Bash ツールが打つコマンド文字列を検査していた。しかし 2026-09-11 の kill は `bash <script>.sh` の**内部**で起きたため、Bash ツールに渡ったコマンド文字列には `tmux` も `kill-server` も現れず、**文字列検査では原理的に捕まえられなかった**。

構造的な結論: 「コマンド文字列を見る層」は script 内部の呼び出しを見られない。**実行時に tmux 呼び出しそのものを傍受する層**が要る。

## いまの二層防御

### 第一防御: `bin/tmux` shim (実行時傍受)

`~/dotfiles/bin` は PATH 先頭 (homebrew の `/opt/homebrew/bin` より前) にあるので、`bin/tmux` を置くと **あらゆる `tmux` 呼び出し (スクリプト内部の bare `tmux` を含む) がまずこの shim を通る**。これが script 内部の kill を捕まえられる唯一の層。

shim の判断 (`bin/tmux` が出典):

1. **fast path**: kill 系トークンを含まない呼び出しは、解析も fork も一切せず即 `exec` で実体 tmux へ渡す (precmd/statusline が高頻度に叩くため。実測の追加コストは約 6.4ms/call、fork ゼロ)。
2. **target socket の解決**: `-S <path>` / `-L <name>` / `$TMUX` / default の順に、tmux と同じ規則で対象 socket を決める。**`TMUX_TMPDIR` が空・不在なら /tmp へフォールバック**する tmux の挙動を再現する (2026-09-11 の経路そのもの)。ディレクトリを `pwd -P` で正規化 (macOS では `/tmp`→`/private/tmp`) し、**比較は小文字化して行う** (macOS の FS は case-insensitive で `default` と `DEFAULT` は同一ファイル = 同一サーバ)。target を特定できなければ **fail-closed で拒否**する。
3. **保護対象か判定**: 対象 socket が `default` の正規パス、または `~/.config/tmux-protected-sockets` の各行と (case-insensitive で) 一致するか。しなければ素通し (隔離テストサーバ `tt-cleanup-*` / `ctrlv-test-*` / `-L <name>-$$` はここを通る)。
4. **保護対象への kill の扱い**:
   - **kill-server (サーバごと全滅)**: TTY の有無に関わらず**無条件で拒否**。
   - **kill-session (個別セッションの片付け = 日常操作)**: 対話 TTY のとき許可、非対話 (Claude/script) なら拒否。

### 第二防御: `deny-bare-tmux-kill.sh` (コマンド文字列)

Claude が**直接打った** Bash コマンドを境界で検査し、ソケット未指定の kill と `-L default` / `-S <...>/default` (大小問わず) の本番直撃を deny する。script 内部は見られない (そこは第一防御が担う) が、直叩きを二重に止める。

## エスケープ (本当に本番を消したいとき)

- **kill-server**: 実体を絶対パスで呼ぶ。`/opt/homebrew/bin/tmux kill-server`。これが唯一の経路 (対話 TTY でも shim は kill-server を通さない)。
- **kill-session**: 対話シェル (端末) で `tmux kill-session -t <name>` を打てば通る。日常のセッション整理はこれで動く。

絶対パス呼び出しは shim を通らないので、**意図的なエスケープ**として機能する (grep 可能・記憶しやすい)。脅威モデルは「うっかり」であって「対人の防御」ではない。

## 名前付きサーバを守りたいとき

`~/.config/tmux-protected-sockets` に守りたい socket の正規パスを 1 行 1 個書く (`#` でコメント可)。shim がその socket への非対話 kill も拒否する。

## 何を守れて、何を守れないか (残存リスク)

守れる (敵対レビューで実測):
- 2026-09-11 の実際の経路 (subagent → `bash script.sh` → TTY 無し → `-L default` + 空 `TMUX_TMPDIR`) は拒否される。
- `/tmp` ↔ `/private/tmp`、`..`、二重/末尾スラッシュ、**大文字小文字違いの綴り** (`-L DEFAULT`) はすべて正規化・小文字化で吸収して拒否。
- **`;` / `\;` で連ねた複数コマンド** (`list-sessions \; kill-server` のように非 kill の後ろに kill を隠す形。TTY 不要) も、連鎖の全 subcommand を走査して拒否。**内部空白の無い密着末尾 `;`** (`kill-server;` — tmux はこれを終端扱いして実行する。decoy 実測) も token を正規化して拒否。内部空白のある `kill-server ;` は tmux が実行しないので素通し (無害)。
- **`kill-session -a`** (指定 1 個以外を全滅 = catastrophic) は server 扱いに昇格して無条件拒否。
- 隔離テストサーバ (名前ベース) を誤って守ることはない (過剰 deny なし。連鎖でも隔離サーバなら素通し)。

守れない (既知・意図的にスコープ外):
- **実体を絶対パスで呼ぶ形** — 正規のエスケープ。意図的回避は脅威に含めない。
- **pty がチェーンに入った状態での単一 kill-session** — `script`/`expect`/pty driver/tmux ペイン内では `[ -t ]` が TTY を返すため、kill-session の TTY ゲートが非対話でも開く。kill-server と `kill-session -a` (catastrophic) はこの穴を負わないよう TTY を通さない。残るのは**個別 1 セッションの kill-session** だけで (`-a` なし)、これは housekeeping 優先で許可を残している。1 セッションずつ消す形は非 catastrophic。
- **REAL のハードコード** (`/opt/homebrew/bin/tmux`) — tmux が別位置に移ると全 tmux 呼び出しが失敗する。Homebrew の symlink は版更新をまたいで安定なので現実的リスクは低い。
- **第二防御 (hook) の basename 判定** — hook は文字列検査なので、`-S <非本番dir>/default` のように basename が `default` の隔離 socket を過剰 deny しうる。第一防御 (shim) は full-path で正しく判定するので、実行時の最終判断は shim が持つ。

## 検証

- `tests/tmux/test_tmux_shim_protects_default.sh` — shim の決定 (block/素通し) をスタブ差し替えコピーで、実 exec の load-bearing をデコイ隔離サーバで確認 (本番不触)。今日の正確な形と大文字綴りを含む。
- `tests/claude/test_deny_bare_tmux_kill.sh` — hook の deny/allow を pin。
- 設計と実装は Opus の敵対的レビュー (red team) を最終ゲートに通し、指摘 P1 (大文字綴りの貫通) / P2 (pty の TTY ゲート越え) を反映済み。

## 関連

- `_claude/rules/tmux-probe-requires-socket-isolation.md` — 破壊的 tmux コマンドの隔離規範 (この二層はその「強制手段」節)。
- `docs/tmux-plugins.md` — セッション永続化 (resurrect + continuum)。誤殺されても復元できるのはこの機構による。
