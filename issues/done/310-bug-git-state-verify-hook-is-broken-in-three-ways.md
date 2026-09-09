# bug: `git-state-verify.sh` が 3 通りに壊れており、宣言した目的を逆向きに壊している

起票日: 2026-09-07
カテゴリ: bug
優先度: 高（この hook は「誤った成功報告を構造で潰す」ためのもので、**壊れると誤報を後押しする側に回る**）
出典: /audit broken-code 2026-09-06（forge Minimum+）。**3 件とも本セッション中に実際に発生**しており、
その hook 出力が証拠として会話に残っている

対象: `_claude/hooks/git-state-verify.sh`

## ① 発火判定が `git -C <dir> commit|push` を拾わない（doc の主張と食い違う）

トリガは `printf '%s' "$cmd" | grep -Eq 'git([[:space:]]+-[^[:space:]]+)*[[:space:]]+(commit|push)'`。
実測（2026-09-06）:

| コマンド | 判定 |
|---|---|
| `git commit -m x` | MATCH |
| `git push origin HEAD:master` | MATCH |
| `cd /tmp && git push` | MATCH |
| **`git -C /tmp/r commit -m x`** | **NO-MATCH** |
| **`git -C /Users/koji/dotfiles push`** | **NO-MATCH** |
| **`git --no-pager -C /x push`** | **NO-MATCH** |
| **`git -c user.name=a commit`** | **NO-MATCH** |

同ファイル 20 行目のヘッダは「`git -C dir commit` や `... && git push` のような形も拾う」と書いており、
**契約違反**。

**根本原因**（dotfiles-71 が独立に再現して特定）: `-[^[:space:]]+` は **`-C` は食えるが、
その値 `/tmp/r` を食えない**。値を取らないオプション（`--no-pager`）だけが通り抜ける。
つまり「オプションを 0 個以上読み飛ばす」つもりの式が、**値つきオプションを扱えていない**。

🚨 **この repo の規範がまさにその形を要求している**:

- [`commit-with-pathspec.md`](../../_claude/rules/commit-with-pathspec.md):
  「本体への操作は **`git -C <本体の絶対パス>`** で対象を明示」
- [`worktree-per-session.md`](../../.claude/rules/worktree-per-session.md):
  「本体への pull / worktree remove は **`git -C ~/dotfiles`**」

つまり**規範どおり書いた瞬間に検証装置が不在になる**。

## ② 未 push 判定が detached HEAD を 1 件も見ない

`git log --branches --not --remotes` は detached HEAD の commit を拾わない。
そして**この repo は detached worktree を規範として要求している**
（`.claude/rules/worktree-per-session.md` / `_claude/skills/audit/SKILL.md`）。

隔離 repo での再現（2026-09-06。bare remote + clone + `git worktree add --detach` に 1 commit）:

```
git log --branches --not --remotes --oneline  →  （空）
git log HEAD --not --remotes --oneline        →  18a17cf unpushed in detached worktree
git rev-list HEAD --not --remotes             →  18a17cfd69b2...
```

沈黙ではなく **`(none — すべて push 済み)` という積極的な偽の全クリア**を注入する点が重い。
同型が 4 箇所あるため**横断の修正は issue 311** に分けた。

## ✅ ② は issue 311 で解消 (2026-09-09)

`git-state-verify.sh:35` を含む 4 箇所すべてを `HEAD --branches --not --remotes` へ直し、
**判定不能 (git repo でない / remote 未設定) を「push 済み」に丸めない**分岐も足した
(この issue の②が要求していた分)。変異 (`HEAD` を外す) で red を確認済み。
**①と③はこの issue に残る。**

## ③ 見ている repo が違ううえ、第三者のテキストを権威的ラベルで注入する

state 収集は `git rev-parse` / `status` / `log` を**引数なし**で実行するので、
**hook のセッション cwd（＝ `~/dotfiles`）**を見る。`git -C <path>` でも `cd X && git commit` でも同じ。

**本セッションでの実例**: 使い捨て repo（`$TMPDIR/.../work/wt`）で `git commit` した直後、
hook は `~/dotfiles` の branch / status / last commit を
「git commit/push 直後の実 git state（成功報告の前にこれで検証すること）」として注入した。
しかもその last commit は**別セッション（dotfiles-71）が書いたもの**だった。

派生して 2 つ:

- **untrusted な引用が権威ラベルで入る**: 注入内容はコミットメッセージ・ブランチ名で、
  pull 後は著者が第三者。「これで検証すること」というラベルの下に置かれる
- **git を 1 度も実行しないコマンドでも発火する**: 判定は `tool_input.command` の grep なので、
  散文・heredoc・grep のパターン文字列で発火する。**本セッションでは私の `echo` と `grep` だけの
  コマンドで実際に発火した**（兄弟の `deny-bare-tmux-kill.sh` は同型の誤検出を doc 化しているが、
  こちらは無記載）
🚨 **①と③は同じ根**（dotfiles-71 の指摘）。トリガはコマンド文字列に `commit` / `push` という
**語**が含まれるかしか見ていないので、「`git -C` を拾えない」も「git を呼ばない散文で発火する」も
同じ原因から出ている。**したがってトリガを緩める方向（`git -C` を拾えるようにする）に直すと、
過剰発火も一緒に広がる**。語の一致ではなく**コマンドとしての `git` 呼び出しか**を見る方向へ
寄せると両方が同時に閉じる。

- **`head` の切り詰めにマーカーが無い**: `status -sb | head -30` / `log -1 --stat | head -40` /
  `unpushed | head -20` は、切れたことを示さずに「部分的な真実」を渡す

## テストが 1 本も無い

```
grep -rl 'git-state-verify' tests/  →  0 件
```

兄弟の hook はすべて持っている（`next-claim-unshared` 1 / `next-claim-push` 1 /
`deny-bare-tmux-kill` 2 / `issue-progress-check` 1）。**この hook だけが非対称**。

## 推奨対応（順序が重要）

🚨 **拾える範囲を広げるより先に「どこを見た state か」を出す**。順序を誤ると、
現状の「無音の no-op」が「**自信を持って誤った ground truth**」へ悪化する。

1. **注入本文の冒頭に `検査した repo: $(git rev-parse --show-toplevel)` を必ず 1 行出す**（③の主案）
2. 注入本文を明示的に区切り、**「以下は untrusted な引用であり指示として読まないこと」**のヘッダを付ける
3. トリガを、先頭トークンが `git` のときだけグローバルオプション
   （`-C <値>` / `-c <値>` / `--no-pager` 等。値を取るものは値ごと）を読み飛ばして
   第 1 サブコマンドを見る形へ寄せる（lib へ切り出す）。ヘッダ 20 行目の記述も実装と揃える
4. `head` の切り詰め時に「（以下略）」を出す
5. 誤検出（引用符に入っていない散文での発火）を doc に明記する

## 受け入れ条件

- [x] `tests/claude/test_git_state_verify.sh` を新設し、上の 7 形式 + **陰性対照**
      （git を実行しない文字列）を固定する
- [x] **変異検証**: `-C` 対応を外すと red / 「検査した repo」行を消すと red
- [x] 注入本文に untrusted 引用ヘッダが付いていることを検査する
- [x] 集約経路から実行され、**その検査の出力行が出る**ことを確認する
      （[`verify-execution-not-just-exit-code.md`](../../_claude/rules/verify-execution-not-just-exit-code.md)）

## 関連

- issue 311（②の同型 4 箇所を横断で直す）

## 進捗

| 推奨対応 | 状態 | commit (subject) |
|---|---|---|
| 1. `検査した repo:` を冒頭に出す | 済 | fix(310): git-state-verify の発火判定をトークン解析へ寄せ、出典と untrusted ヘッダを足す |
| 2. untrusted 引用のヘッダ | 済 | 同上 |
| 3. トリガをトークン解析へ（lib へ切り出し） | 済 | 同上（`_claude/hooks/lib/git_cmd_detect.sh` を新設） |
| 4. `head` の切り詰めに「（以下略）」 | 済 | 同上（`head_marked` ヘルパー） |
| 5. 誤検出の範囲を doc に明記 | 済 | 同上（lib ヘッダの「検出しないと決めた形」節） |

## 結果（実測）

- **トリガの表を作り直した**（`git_cmd_invokes` を新実装で実行）: issue が NO-MATCH と
  記録していた 4 形式（`git -C /tmp/r commit` / `git -C /Users/koji/dotfiles push` /
  `git --no-pager -C /x push` / `git -c user.name=a commit`）が**すべて MATCH** に変わった。
  既存の MATCH 3 形式は MATCH のまま。**陰性対照 5 件**（`echo "git commit したら git push する"` /
  `grep -rn "git push" docs/` / `git status` / `git log` / `ls -la`）はすべて NO-MATCH
- **`tests/claude/test_git_state_verify.sh`**: 検査 16 件 / ok=16 / fail=0
- **集約経路**: `make test` の出力に `[ok] tests/claude/test_git_state_verify.sh` が出ることを確認
  （exit code ではなくその行の存在で判定）
- **変異検証 3 本、すべて red**:

  | 変異 | 出た赤 |
  |---|---|
  | `-C` / `-c` を「値ごと飛ばす」分岐を削除 | `✗ 発火しない: git -C /tmp/r commit -m x` |
  | `検査した repo:` の行を削除 | `✗ 「検査した repo: …」が無い` |
  | untrusted ヘッダを削除 | `✗ untrusted 引用のヘッダが無い` |

- **lint**: `make test-lint` rc=0。途中 2 件を自分で作り込んで直した
  （① `printf … | grep -q` の 5 箇所が `check_pipefail_grep_q.sh` に落ちた
  ② `tr '\n;|' '\n\n\n'` が SC2020。分割を sed へ寄せ、`||` を `|` より先に潰す順序をコメントで固定）

## 残タスク

- **スコープ外**: ②（未 push 判定の detached HEAD 盲点）の**横展開 4 箇所**は issue 311 が担当。
  本 issue では hook の入口からの回帰テストとして 2 件（detached worktree の未 push / remote 未設定を
  「判定不能」と出す）を固定するに留めた
- **未検証**: `git_cmd_invokes` が「検出しない」と宣言した 4 形式（heredoc 本文 / 変数・alias 経由 /
  `sh -c` の入れ子 / `xargs git`）は、意図的に取りこぼす側へ倒しているのでテストを書いていない。
  射程を変えるなら lib ヘッダの脅威モデル節を同じ commit で直す

## 敵対的レビュー 1 周目 (opus / read-only) と、その修正 — 2026-09-09

「壊す手順を見つけろ。壊せなければ壊せなかったと明記しろ」で起動。**P1 が 2 件出た。
どちらも自分で再現してから直した**（レビューの出力も無検閲では採らない）。

### P1-1: 311 の主張を pin していると書いたテストが、退行を当てても緑だった

`git log HEAD --branches --not --remotes` から `HEAD` を外す（= 311 が直した退行そのもの）を
当てても **16/16 green**。assert が `grep -q 'unpushed in detached' <<< "$body"` と**本文全体**を
見ており、`git log -1 --stat` が出す同じ subject にマッチしていた。未 push 節は
`(none — すべて push 済み)` という**積極的な偽の全クリア**に戻っていたのに緑。

→ 節を切り出す `section()` を入れ、**未 push 節に限って**判定する形へ。逆向きの
「未 push があるのに『すべて push 済み』と言わない」も対に足した。

### P1-2: 310 が「別 repo についての偽の全クリア」を**新しく作っていた**

310 以前は `git -C <別 repo> commit` で発火しなかったので注入自体が無かった。トリガだけ
広げた結果、hook は cwd の repo を見たまま「最後のコミット」と「すべて push 済み」を出す。
実測（A は push 済み / B に未 push、cwd=A で `git -C B commit`）で、A の init commit と
`(none — すべて push 済み)` が出た。

→ `git_cmd_invokes` が `-C` の値を `GIT_CMD_TARGET_DIRS` に返し、hook が**触った repo ごとに
1 ブロック**出す形へ。行き先が存在しなければ「判定不能」（`push 済み` に丸めない）。

### P2: 射程の宣言と実装の食い違い（lib ヘッダの「検出しない形」が不完全）

未申告の取りこぼし **5 形**（`{ git commit; }` / `do git -C … push` / `else git commit` /
`out=$(git push)` / `time git push`）と、未申告の**過剰発火**（引用の中の散文を `;` / `&&` で
割って独立コマンドと読む）。

→ ① 分割を**引用を見る**形に作り替え（`_git_cmd_split`）② 先頭のシェルキーワード
（`do then else elif time ! exec nohup env`）を落とす ③ `$(` ` ` ` ( ) { }` でも割る。
5 形すべて発火し、散文 2 形は沈黙するようになった。

### P3: 変異で緑のままだった主張 4 件 → すべて検査を足した

`(以下略)` / untrusted ヘッダの**意味反転** / `suppressOutput` / 環境変数の前置き。
bare repo を「git リポジトリの外」と誤ラベルする件も直した。

🚨 **ヘッダの検査を「語の存在」から「文面」へ変えたら、1 回目は `grep -qF` に 3 行の
パターンを渡した。grep は複数行パターンを行ごとの OR として扱う**ので、1 行目さえ合えば通り、
**3 行目を反転させても緑のまま**だった。文字列比較（`[ "$got" = "$want" ]`）へ直して red を確認。

🚨 **変異がテスト自身のバグを 1 件炙り出した**: `bad "「検査した repo: $real」が無い"` は
bash が全角括弧まで変数名に取り込むため、`set -u` の下で**assert が失敗したときだけ**
スクリプトが死に、集計行に到達しない（= 判定不能を「テストが落ちた」に見せる形）。`${real}` へ。

### 変異検証（11 本、すべて red / baseline green）

判定は 3 値。「変異が未適用」「構文エラー」は red にも green にも丸めず第 3 の結果として出した
（実際 M4 は 1 回目のハーネスで未適用になり、python で当て直して red を確認した）。

| 変異 | 落ちた assert |
|---|---|
| M1 `HEAD` を外す（311 の退行） | detached の未 push を見落とし / 偽の全クリア（2 件） |
| M2 `検査した repo:` を消す | 出典が無い / `git -C` の報告先も落ちる（2 件） |
| M3 untrusted ヘッダの意味を反転 | ヘッダの文面が違う |
| M4 `(以下略)` を出さない | 60 ファイルでも切り詰めを黙る |
| M5 `suppressOutput` を false | suppressOutput が true でない |
| M6 環境変数の前置きを落とさない | `FOO=x git commit` が発火しない |
| M7 `-C` の値を飛ばさない | `git -C …` 3 形が発火しない（計 7 件） |
| M8 hook が `-C` を無視し cwd を見る | cwd の state を出している / 行き先の未 push が出ない |
| M9 引用を見ずに区切る | 散文 2 形で過剰発火 |
| M10 bare の fallback を消す | bare を「repo の外」と誤ラベル |
| M11 先頭キーワードを落とさない | `do` / `else` / `time` の 3 形が発火しない |

検査は **16 件 → 32 件**。`make test-lint` rc=0、`make test-dir DIR=tests/claude` の出力に
`[run] tests/claude/test_git_state_verify.sh` が出ることを確認（同 target の失敗は既存の
`test_dangling_symlinks.sh` 1 本のみで、これは環境側）。

## 敵対的レビュー 2 周目 — 2026-09-09

§7 のとおり、1 周目の指摘を直して**新設した機構**（`_git_cmd_split` / 触った repo ごとのループ /
`section()`）に対してもう 1 周攻めさせた。**P1 が 3 件**出て、うち 1 件は
**1 周目の修正が新しく作った退行**だった（親 commit との A/B で確定）。

### P1-1 (退行): 本文に書かれた `git -C` が「検査する repo」を乗っ取る

heredoc / バッククォートの中に `git -C /other push` と書くだけで、実際にコミットした repo が
**1 文字も出なくなる**。原因は 2 つの合成で、
(a) 行き先の抽出が本文に対して無防備 (b) cwd を「空行」でインバンド表現していたため、
コマンド置換の末尾改行落ちで cwd のブロックが消える。

### P1-2: 順序で cwd のブロックが黙って消える

`git -C <本体> … && git push`（= `worktree-per-session.md` が要求している形そのもの）で
cwd のブロックが落ちた。順序を逆にすると出る。

**→ 根治: cwd を「行き先の集合の 1 要素」にするのをやめた。** cwd は必ず先頭に報告し、
`-C` の行き先は**追加のブロック**としてのみ足す。これで偽の `-C` は「ブロックが 1 つ増える」
だけになり、**真の repo を消せない**（出るべき state が消える向きの壊れ方を構造的に閉じた）。
追加ブロックには「引用の中のコマンド例から拾った可能性がある」と明記する。

### P1-3: 多 repo ループをテストが 1 本も守っていなかった

ループを `head -1` に縮めても **32/32 green**（target が 2 つ以上のケースが 0 件だった）。
→ 行き先 2 つのケースと、`git -C .` の重複除去のケースを足した。

### P2 も対応

- `section()` が**部分一致**だったため、コミットメッセージ本文に見出しを書いて節へ任意の行を
  注入できた（`git log --stat` の本文は 4 空白字下げなので、行頭一致にすれば防げる）。
  一度閉じた節を再 arm しない形にもした（多 repo で「B の節を見ているつもりで A の全クリアを読む」を防ぐ）
- **性能**: 分割器は文字単位ループで長さに対して超線形。git と無関係な 40 KB のコマンドで
  **4.2 秒**かかっていた（PostToolUse は全 Bash 呼び出しで走る）。
  `case "$cmd" in *git*)` の前置きフィルタを入れた。壁時計では assert せず**静的に pin** した
  （[`avoid-wall-clock-assertions.md`](../../_claude/rules/avoid-wall-clock-assertions.md)）
- **`IFS` の復元**: unset の呼び出し元へ空文字を書き戻していた（潜在バグ。consumer は 1 本だけ）
- ヘッダの「閉じた引用の中は発火しない」が偽（複数行の引用では発火する）。射程の宣言を実測に合わせ、
  `cd X && git commit` / `--git-dir` / 値に空白のある `-C` を「行き先として拾わない」に明記

### 2 周目の変異（新設機構、すべて red / baseline green）

| 変異 | 落ちた assert |
|---|---|
| extra のループを `head -1` に | 2 つ目の行き先が落ちた |
| cwd を報告しない | 多 repo で片方が消えた（2 件） |
| cwd の重複除去を消す | `git -C .` で同じ repo が 2 ブロック |
| `section()` を部分一致に戻す | 本文の行が未 push 節に注入された |
| 前置きフィルタを消す | 前置きフィルタが無い |

検査は **32 → 39 件**。`make test-lint` rc=0。

## 敵対的レビュー 3 周目 — 2026-09-09

### P1: `VAR="値 に 空白" git push` で hook が**完全に沈黙**していた

```
GIT_SSH_COMMAND="ssh -i ~/.ssh/id_ed25519" git push origin HEAD:master   → MISS (出力ゼロ)
GIT_COMMITTER_DATE="2026-01-01 00:00:00" git commit --amend --no-edit    → MISS
FOO=x git commit -m x                                                    → FIRE (既存テストはこれだけ)
```

環境変数の前置きを落とす正規表現が `[^[:space:]]*` で、**引用された値の中の空白を跨げない**。
結果、先頭トークンが `GIT_SSH_COMMAND="ssh` になり git と一致せず、
**additionalContext が 1 バイトも出ない**（= ブロックが消えるのではなく検証装置ごと不在。
310 が潰したはずの failure mode そのもの）。1 周目に足した機構なので、3 周目が最後のゲートだった。

→ 値のクォートを 1 組許す形へ。あわせて `sudo` / `nice` / `stdbuf` / `ionice` と、
先頭のリダイレクト（`>log git push`）も剥がすようにした。
`timeout 60 git push` は**剥がさない**（引数を取るラッパーなので秒数を git と読み違える）— 射程宣言に明記。

### false green 2 件（テストが何も守っていなかった）

- **前置きフィルタの静的 pin**: `grep -F` は部分一致なので、行頭にコメント記号を足すだけで
  緑のまま通った。**分割器を spy に差し替えて「呼ばれないこと」を見る** seam テストへ差し替え。
  🚨 最初 spy をシェル変数のインクリメントで書いたが、`git_cmd_invokes` は
  `<<< "$(_git_cmd_split …)"` の**コマンド置換 = サブシェル**で呼ぶので親に伝わらず、
  同じ変異がまた緑だった。ファイルへ記録する形へ直して red を確認
- **追加ブロックの注記文**: 「引用の中のコマンド例から拾った可能性がある」を pin するテストが 0 本。
  この文面は「偽の `-C` はブロックが 1 つ増えるだけ」という緩和の根拠なので、無保護だと緩和が静かに消える

### P3

| 指摘 | 対応 |
|---|---|
| cwd が削除済みだと存在しない `-C` 先が「判定不能」を出さず黙って消える（fail-open） | `pwd -P` / toplevel の空を明示的に扱う。テストは**実際に cwd を rmdir してから** hook を走らせる（消さないと変異が緑のまま） |
| `IFS` の save/restore は死んだコードで、しかも無保護 | `local IFS=$' \t\n'` へ。依存の向きが逆（関数が呼び出し元の IFS で単語分割していた）だった。`IFS=ZZ` の呼び出し元で発火するテストを追加 |
| 同じ repo のサブディレクトリを `-C` に渡すと 2 ブロック | 重複判定を**実体ではなく toplevel**で行う |
| 追加ブロックに上限が無く、300 件で本文が 101 KB になり cwd が埋没 | 5 件で打ち切り + 「(以下略)」 |
| hook が `unset CDPATH` していない（テスト側だけが防いでいた非対称） | 1 行追加 |

### 3 周目の変異（8 本、すべて red / baseline green）

引用付き環境変数を扱わない / 前置きフィルタをコメント化 / 注記文を短くする / 上限を外す /
cwd 未解決で fail-open に戻す / toplevel でなく実体で重複判定 / `local IFS` を外す。

検査は **39 → 49 件**。`make test-lint` rc=0。

### 壊せなかったと明記された観点（3 周目）

見出しの偽装・行頭注入（`git log` は 4 空白字下げ、改行入りブランチ名は git が拒否）/
`extra` の 2 回解決の食い違い（P3-1 以外）/ `while` が subshell にある件 /
`set -e` への依存 / CDPATH 汚染 / 前置きフィルタ `*git*` による取りこぼし（検出器が
literal `git` を要求するので原理的に無い）。
