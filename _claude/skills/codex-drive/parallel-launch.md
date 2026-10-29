# codex-drive: 並列起動の作法

> `SKILL.md` (本体) から [S] / [D1] / [D1.5] / [2p] / [3.5] / [3.55] / [3.6] で codex を並べて起動する前に Read する。役割分担・大原則・モデル表・出力の置き場 (`<D>`) は本体にある。節番号 (`[3.8]` など) は本体のワークフローの図と同じ。

## 並列起動の作法（`[S]` / `[D1]` / `[D1.5]` / `[2p]` / `[3.5]` / `[3.55]` / `[3.6]` 共通）

本 skill の並列フェーズはすべてこの作法に従う。各フェーズで再定義しない。

### 既定: driver (`codex-fanout`) で 1 往復に畳む

dotfiles の `bin/codex-fanout` (PATH に載っている)。read-only / review の並列 fan-out + merger を
driver 内で完結させ、Claude の Bash 往復を「起動 1 回 + digest 読み 1 回」にする。

**1 本だけ起動するときは `bin/codex-run`** (codex-fanout の薄い前面。manifest を書かずに済む):

```sh
codex-run -J -C <worktree> -l adv-behavior review <prompt-file>
```

作業ディレクトリは **`-C` オプション (codex-run が cd する)** で渡す。`codex exec review` 自体は
`-C` を受け付けないが、その差分は codex-run が吸収するので呼び出し側から見えない。
`command codex` / `</dev/null` / `--ephemeral -o` / stdout の保存 / rc の保存 / モデルと effort の固定も
codex-run 側で満たす。**rc が 0 でも本文が空なら警告を出す**ので、成果物での判定がそのまま乗る。
`-o` を省くと `./tmp/codex-run/<stamp>-<label>/` に出る (cwd 基準。`-C` の影響を受けない)。本 skill では `-o <D>/<literal-stamp>.<label>` を渡す。

**Go repo では codex の sandbox が `go build` / `go test` を走らせられない**ことがある (work dir / build cache を
`$HOME` 配下に作れず `operation not permitted`、goenv の version 不在。SnapTrim #251 で read-only lens が
ほぼ全本「テスト未実行・静的確認のみ」で返った)。Go を触るタスクでは **起動側で
`GOCACHE=<worktree>/tmp/gocache GOTMPDIR=$(mktemp -d) GOFLAGS=-mod=mod` を export してから
codex / codex-fanout を起動し、prompt にも同じ env を書く** (GOTMPDIR は **repo 外**にする。repo 内にすると Go 1.26 の
`t.TempDir()` の親が repo になり、codex が TestMain に隔離コードを足して環境の都合が production テストに残る: obaket 719) (workspace-write なら worktree 内へ書ける。
read-only は依然走らないので、その lens の「未実行」は Claude が自分で実行して埋める)。
codex の「テストは sandbox で実行不可」は**未検証の印**であって、静的確認を実行結果と読まない。

**Bash ツールの上限 (15 分) を超えうる起動は detach する**: `run_in_background` の Bash は 15 分で
プロセスごと kill される (`CODEX_FANOUT_TIMEOUT=2400` や長い workspace-write 実装と矛盾。2026-08-27 に
敵対 fanout と mutation 単発が同時に全滅した = obaket issues/620)。その場合は
`(nohup codex-fanout … > log 2>&1; echo "rc=$?" > <outdir>.rc) > /dev/null 2>&1 &` で起動し、
Monitor ツールで完了を待つ。**待つのは driver が必ず書く `runs.tsv` (全 run の rc が揃うこと) であって、外側の `<outdir>.rc`
ではない** (run 単位の成果物は全部揃ったのに外側 rc だけ書かれず永遠に待った例: obaket 696 項目 10)。
待機ループには deadline を置き、超えたら「判定不能」として報告する。プロセス不在かつ成果物無し = 死亡として扱う。
15 分以内に収まる起動は従来どおり `run_in_background` でよい。
**自分の run を止めるときは PID か `-o` の出力パスで特定する** (`kill <pid>` / `pkill -f '<自分の -o パス>'`)。
`pkill -f 'codex exec -s danger-full-access'` のような pattern kill は**同じマシンの別セッションの codex を巻き込む**
(2026-09-05 obaket: 別セッションの pattern kill で実装 run が途中で死んだ。編集は残っていたので突き合わせで復旧したが、
kill された側からは「codex が壊れた」に見える)。
**outdir / log / manifest は repo root からの絶対パスをリテラルで書く** (`$PWD` 禁止。Bash の cwd は呼び出し間で
primary cwd に戻り、別 repo の tmp に出る: obaket 696 項目 1 / 715 項目 5)。

Claude がやるのは:

1. プロンプト部品を `<D>` に書く — 定型は `~/.claude/skills/codex-drive/templates/`
   (`review-lens-header.md` = lens 共通ヘッダ、`merger.md` = merger の既定指示) を使い、
   **Claude が書くのは lens の攻め口 + タスク固有の的の brief 数行だけ** (トークン経済 2)
2. manifest (TSV: `label \t mode \t model \t effort \t 部品1,部品2,... [\t timeout_s]`。部品は順に連結) を書く。
   **`mode` は `ro` | `review` の 2 値** (それぞれ `codex exec -s read-only` / `codex exec review`)。
   下の例はコマンド形で書いてあるので、manifest には略号の方を書くこと。
   末尾の `timeout_s` は任意で、**その行だけ** timeout を上書きする (省略時は `CODEX_FANOUT_TIMEOUT`)。
   敵対レビュー系 lens は反証の構築に時間がかかり既定値で時間切れ (rc=124) になりやすい (dotfiles issue 150) ので
   `2400` を付ける ([3.6] が正本)
3. **manifest・prompt 部品・merger prompt (`-m`)・outdir はすべて cwd 依存**なので、各 Bash 呼び出しで
   値を再計算するか、前回出力のリテラル絶対パスを使う。prompt part のパスにはカンマを入れない
   (manifest は単純なカンマ分割)。`codex-fanout -J <絶対パスの manifest> <絶対パスの outdir>` を、
   同じ呼び出しで必要なファイルを生成してから **1 回だけ `run_in_background` で起動** (2 本以下で merger 不要なら `-M`。
   1 本だけの manifest は `-m` 明示が無い限り driver が merger を自動スキップする — 1 出力の digest は言い換えでしかない)
   生成した manifest はその呼び出しで即実行する。manifest を残しておいて後で再実行すると、repo 移動・
   別 worktree・別マシンで stale path になる (出典: `573-retro-codex-drive-541-2026-08-24.md`)。
4. 完了通知後に `<outdir>/digest.md` を読む (原文へは digest の出典パスで跳ぶ)

**起動と同時に「待機中に自分がやる作業」を決める。** read-only フェーズは実測で
20〜40 分かかることがあり、その間 main agent が「待機中です」のターンを重ねるのは丸損
(1 ターンごとに会話全体を再送する)。起動した直後に、その codex の結果に**依存しない**作業を
1 つ選んで着手する — issue / commit message のドラフトを `<D>` に書く、自分でできる裏取り
(grep・件数の計測・ログの取得) を済ませる、次のフェーズのプロンプト部品を組む。
待ち自体は**成果物ファイルの存在**で張る (`until [ -f <outdir>/runs.tsv ]`。プロセスの存在で
待たない — [`verify-execution-not-just-exit-code.md`](../../rules/verify-execution-not-just-exit-code.md))。
実測 2026-09-08 (swift-smbee 087): 待機中に自分で数えた「重複 25 行」が、
後から届いたレビュワーの計数 (別範囲) の誤りを弾く根拠になった。

- **exit code を成功判定に使う**: 0 = 全本 + merger 成功 / 2 = 一部失敗 (digest はあるが観点が欠けている —
  **成功扱いしない**。欠けた観点を再実行するか「未実施」を Claude が明示的に引き継ぐ) / 1 = 全滅・merger 失敗
  - codex の利用上限で落ちた run は rc を変えずに、`runs.tsv` の `cause` 列が `usage_limit`・要約が `label:rc=N(利用上限)` になる
    (`ratelimit -check` が見ない枠。prompt や観点を直しても通らないので、再開の時刻を待って回し直す。検出の範囲は `bin/codex-fanout` の `usage_limit_line`)
- 🚨 **`codex-fanout` に `-C` は無く、各 run は driver を起動した cwd の木を読む**。レビュー対象が worktree なら、同じ Bash 呼び出しで
  `cd <worktree の絶対パス> && codex-fanout …` として起動し、prompt に書く対象のファイルも絶対パスにする (`tmp/…` や `git show HEAD` の
  相対の指定は、起動した場所の木を指す。obaket retro 1112: 共有 checkout から起動し、codex が自分で worktree を探したので結果は使えたが、
  探し当てる保証は無い)。1 本だけなら `codex-run -C` で足りる
- driver は read-only / review 専用。**workspace-write の並列 (`[2p]`) は対象外** (worktree 分離が必要。[2p] の手順に従う)
- `-J` で JSONL を保存し、`<label>.meta.json` の完了状態・thread ID・usage・command_execution を確認する。
  機械集約が必要なら `-S <schema.json>` を ro run に付ける (全行に同じ schema を使える manifest のみ)。
  詳細は codex-review「自動化向けの出力」。merger は指摘 ID の対応を保持し、元 run の未確認状態を格上げしない。
- manifest 形式・タイムアウト (`CODEX_FANOUT_TIMEOUT`)・出力レイアウトの正本は `bin/codex-fanout` 冒頭の usage

### fallback: per-call 方式 (driver が使えない環境のみ)

**「2 本だから driver は大げさ」で素の `codex exec` を手で組まない。** 2 本以下でも
`codex-fanout -M` か `codex-run` を使う (集約規約が言う「2 本以下は merger 不要」は
**digest を挟むかどうか**の話であって、起動を手組みしてよいという意味ではない)。
手組みに逃げると下の作法を毎回自分で再現することになり、その一つでも落とすと空振りする。

- **1 本 = 1 つの Bash 呼び出し (`run_in_background: true`)**。1 つのシェルに複数行並べると直列実行になる。
- **シェル変数は Bash 呼び出しをまたいで保持されない**。`stamp="$(date ...)"` を先の呼び出しで作って後続で
  `$stamp` を参照すると**空文字に化ける** (worktree のパスに使っていると存在しないディレクトリを掴んで即死する)。
  最初の呼び出しでパスを確定したら、**以降の呼び出しにはリテラルの絶対パスを書く** (値を会話に控えてから使う)。
  driver に渡すパス (manifest / prompt 部品 / merger prompt / outdir) の扱いは
  **上の「既定: driver」の手順 3 が正本**。ここでは繰り返さない。
  この罠は [2p] の worktree 準備 (driver 対象外) でも踏むので、driver 有無に関わらず覚えておく。
- **起動前にレビュー対象の commit/base を SHA に解決して固定する**。`git log --grep` は
  候補を探すためだけに使い、複数一致を勝手に選ばない。実行中に rebase・編集が入ったら、
  対象が変わったレビューを現行コードの合格根拠にせず、旧 SHA の指摘として扱って差分を再確認する。
- **出力は 1 本ごとに一意パス**。並列で同じパスに書かせない。
- **stdout/stderr も一意ファイルに保存してから末尾を見る**。`codex exec review` はレビュー本文を `-o` に書かず
  stdout にだけ出すことがある (正本: codex-review スキル「手順 4」)。`| tail -40` だけで受けると本文前半を捨てるため、
  **重要な反証を読み落としたまま「指摘なし」と誤判定する**。形は `... > "$log" 2>&1; tail -40 "$log"` にし、
  判断の入力は必ず保存したファイル (下記 merger 経由なら merger がその全文) にする。
- **集約 (merger) 規約 — 並列 3 本以上のフェーズは digest 化してから Claude が読む**:
  全本の完了後、merger codex (`codex exec -s read-only`・`gpt-6-astra`・medium) に**保存ログのファイルパス群を
  渡して読ませ**、1 枚の digest を作らせる — 指摘/案ごとに「出典ログパス・file:line・根本原因・発火条件・
  重複マージの結果・severity」を構造化し、**全数勘定を必須にする** (「run X: N 件中 M 件採用、落とした分は
  各 1 行の理由」+ 指摘ゼロの run の明記。merger の黙った取りこぼしを構造的に見えるようにする)。
  **merger に tail や抜粋を渡さない** (本文が `-o` に出ない罠は、merger が保存ファイル全文を読むことで塞ぐ)。
  Claude は digest を読んで採否し、**採用する指摘・digest が曖昧/矛盾する項目だけ**出典ログの原文へ跳ぶ。
  merger 自体の起動は codex 消費 (経済 1)。driver 経由ならこの規約は `templates/merger.md` が実装済み。
  2 本以下のフェーズは merger を挟まず Claude が直接読んでよい (集約のオーバーヘッドが逆転する)。
- **本数の上限は「merger digest + spot check を Claude が精読できるか」で決める**。最終判断は委譲できず律速になる。

> この作法は各 Phase のファイルから参照される。並列で起動する Phase に戻って続ける。
