# 670 (refactor): 内部で使う Python スクリプトを Go に置き換える

> 🚨 **担当中: epic 674 を直列で進めているセッション**（2026-10-08〜）

起票日: 2026-10-08

## 概要

dotfiles の中で道具が内部で呼ぶ Python (`bin/lib/codex-events.py` など) を、順に Go へ置き換える。
ユーザーの方針 (2026-10-08): 「指定したファイル以外にも、内部で使うスクリプトも Go で置き換えていきたい」。

🚨 **前提**: `bin/lib/codex-events.py` と `tests/test_codex_events.py` / `tests/claude/test_codex_events.sh` は commit
`feat(codex): record structured run events and reject incomplete results` で入る。2026-10-08 の起票時点では、この commit は
別セッションの手元にしか無く、origin/master には載っていない (`git branch -r --contains` が空)。着手前に master に載ったかを確かめる。

## なぜ Go か

1 本ずつ見ると Python のままで困っていないものが多い (`codex-events.py` は 84 行で、標準ライブラリしか使わず、
codex の run の後に 1 回呼ばれるだけ)。それでも寄せる理由は、言語の書きやすさではなく**検査の配線**にある:

- `src/*` の Go には lint / test / CI の lane が揃っている (`src/README.md` の 3 点セット、root の `make test-go`)
- Python には lint (ruff / mypy / flake8 / pylint) の配線が無い (Makefile・`.github/workflows/` に 0 件、`pyproject.toml` / `ruff.toml` も無い。2026-10-08)。
  テストは `tests/**/test_*.sh` から `python3` を呼ぶ形でだけ走る (例: `tests/claude/test_codex_events.sh`)。
  構文検査は `bin/mutate-verify` の `py_compile` があるが、変異させる相手にしか効かない
- LLM が書く・直す前提では、型とコンパイルが typo・型の取り違えを実行前に止める方が安い
  (`codex-events.py` が `isinstance(..., dict)` を手で書いているのは、型が無い分を手で補っている形)
- 同じ判定を Go 側からも読みたくなったとき (glogx / ratelimit が codex の run の結果を読む等)、2 言語で同じ解析を持たずに済む

## 棚卸し (2026-10-08)

数え方: `git ls-files '*.py'` + 拡張子なしで shebang が python のもの + `git grep -nE '(^|[^a-zA-Z_/.-])python3?( |$|")'`
(issues / docs / md / コメント行を除く)。

### 置き換えの対象

| ファイル | 行数 | 呼び出し元 | 備考 |
|---|---|---|---|
| `bin/lib/codex-events.py` | 84 | `bin/codex-fanout` (`:194` `event_inspector`、`:243` で呼ぶ) | **最初の 1 本**。テストは `tests/test_codex_events.py` (174 行、`EventTests` / `DriverTests`) |
| `bin/codex-fanout` の python3 の事前確認 (`:100`) と schema 検査 (`:104` `python3 -c`) | 2 行 | 同ファイル | codex-events と一緒に外さないと python3 依存が残る |
| `zshlib/_concat_helpers.zsh:397` (`__concat_mp4_effective_size` の `python3 -c`) | heredoc 1 本 | 同ファイル | **実行時に呼ばれる本体コード**。mp4 のトップレベル box を読む |
| `bin/tmux-toast` の `python3 -c` (`:94` 表示セル幅、`:185` tick 数) | 2 か所 | 同ファイル | 置き換えではなく**挙動の変更**になる (下の「tmux-toast」) |
| `bin/repair_avi_vorbis_audio.sh:91` の `python3 -` (Ogg の組み直し) | heredoc 1 本 | 同ファイル | |
| `src/lockman/ab_abandoned.sh:37` の `python3 -` (A-B 用に main.go を書き換える) | heredoc 1 本 | 手動の計測ハーネス (`src/lockman/README.md`) | 優先度は低い |

### Go にしない .py (特定のツールのためだけにあり、単体で完結するもの)

2026-10-08 のユーザーの方針: 「特定のツールのために使っていて、単発で完結している py はそのままでいい」。
線引き: **単体で起動して仕事が終わるもの**はそのまま残す。**別の道具の処理の一部として毎回呼ばれ、その出力で呼び出し元が判定するもの**
(`bin/lib/codex-events.py` は `bin/codex-fanout` の run ごとに呼ばれ、complete / incomplete の判定を返す) は移す対象に残す。

| ファイル | 行数 | 何のためか |
|---|---|---|
| `bin/repair-avcc-avi` | 706 | 再生できない AVI (AVCC 形式の H.264) の修復。Finder メニュー (`@DotfilesSyncer`) から単体で起動する |
| `src/chromecookie/mutation_check.py` | 208 | chromecookie の安全装置の変異検証。手動で単体実行する (参照 0 件)。`bin/mutate-verify` へ寄せるかは別の判断で、この issue では扱わない |
| `tests/setup/lib/make_terminal_fixtures.py` | 96 | Terminal のプロファイルのテスト (`tests/setup/test_terminal_profile_{appearance,restore}.sh`) 用の fixture を生成する |
| `_claude/skills/zundamon-kaisetsu/scripts/psd_faces.py` | 109 | zundamon-kaisetsu の立ち絵を PSD から書き出す。`psd-tools` (`uv run --with psd-tools`) に依存する |
| `src/pro-con/samples/*/` の 9 本 (`485-dependency-view/pro-con-485-sample.py` / `509-upgrade-transition/sample.py` / `515-help-flow/sample.py` / `531-question-card/sample.py` / `532-card-search/sample.py` / `537-picker-cards/sample.py` / `543-join-no-owner/sample.py` / `556-card-color/sample.py` / `556-card-color/palette.py`) | 43〜414 | pro-con の見た目を決めるための使い捨てのサンプルレンダラ (`decide-layout-in-sample-renderer-first.md`) |

再評価の trigger: その .py が 2 つ目の道具から呼ばれるようになったとき / 別の道具の処理の一部として組み込まれたとき。

### その他の対象外 (理由つき)

- `bin/mutate-verify:213,221` の `python3 -m py_compile` — 変異させる**相手が** Python のときの構文検査。Python が残る限り必要
- `.github/workflows/doctor.yml:126,129` の `python3 -c` / `python3 -` (出力 JSON の検査) — CI の step の中だけで使っている。
  `jq` で見る形にできるなら一緒に直してよい (優先度は低い)
- `tests/` の `.sh` がインラインで書いている `python3` — テスト側の補助。置き換えた本体のテストを直すときに外せるものだけ外す:
  `tests/bench_stats.sh:66` / `tests/test_bench_stats.sh:17` / `tests/bin/test_kernel_alloc_watch.sh:208` /
  `tests/claude/test_claude_links_sync.sh:297` / `tests/claude/test_deny_piped_push_then_destroy.sh:187,223` /
  `tests/claude/test_tmux_pane_state_bell.sh:208` / `tests/tmux/test_socket_cleanup.sh:273,552`。
  🚨 `tests/tmux/test_tmux_toast.sh` の「python3 不在」のケース (`:304` 付近) は補助ではなく `bin/tmux-toast` の縮退の検査なので、tmux-toast を移すときに直す対象
- **shell スクリプト** (`bin/` 直下の shell 33 本 / `scripts/` 直下の `.sh` 53 本) — この issue では扱わない。
  Go にする基準は下の「どれから移すか」と同じで、当てはまるものが出たら別 issue にする

## 対応方針

### 置き場所 (未決。最初の 1 本で決める)

案を比べて決め、理由を本 issue に書く:

| 案 | 利点 | 欠点 |
|---|---|---|
| A. 小さい道具を集める module を 1 つ作り、道具ごとに `cmd/<name>` (`src/doctor/cmd/svcdoctor` が前例) | 3 点セットが 1 組で済む | go_autobuild の指紋は module root 全体 (`bin/lib/go_autobuild.zsh` の `_go_autobuild_inputs`) なので、道具 A の編集で無関係な B の起動が同期の再ビルドになる。CI の paths filter も module 単位で全部走る。「1 module 1 責務」の `src/README.md` の表と合わない |
| B. 道具ごとに `src/<name>` | 指紋・CI が道具ごとに閉じる | 84 行のために 3 点セット (Makefile・`go.mod`/`go.sum`・`src_<name>.yml`) が 1 組ずつ増える |
| C. 責務の近い既存 module へ寄せる (codex-events なら codex 用の module か `src/ratelimit` の近く) | glogx / ratelimit から import できる package にしやすい | 寄せ先の module の依存と CI に乗る。import するなら `replace` と、使う側の workflow の paths に足す作業が出る |

### 呼び出し経路 (codex-fanout から Go を呼ぶとき)

- `bin/codex-fanout` は bash、`go_autobuild.zsh` は zsh なので、`bin/<name>` の zsh ラッパーを別プロセスで挟む形になる (`bin/svcdoctor` と同じ形)
- 🚨 **バイナリの解決とビルドは codex-fanout の起動時に 1 回で済ませ、絶対パスを持つ** (`runtimeout_resolve` と同じ扱い)。
  今の呼び出し位置 (`run_one` の中、並列の run が終わった直後) でビルドすると、go が無い・ビルドが落ちる・
  lock の競合 (`go_autobuild.zsh` の `_go_autobuild_take_lock … || exit 0`) のどれでも、長い run の判定が失敗に落ちる
- `:100` の「python3 が必要」の事前確認は、go / zsh とビルドの事前確認に置き換える
- ビルドの進捗や失敗の出力が stderr に出る。`.log` に混ぜるかを決める
- PATH や HOME を絞ったテスト環境では GOCACHE が空になり、毎回 cold ビルドになる (`bin/lib/runtimeout.sh` の注記と同じ罠)

### 移し方 (1 本ごと)

1. 旧の Python を**正解役**として残したまま Go 版を書く (`adversarial-review-own-safeguards.md` 0-B)
2. 旧と新に同じ入力を通して出力を突き合わせる。**入力と合格条件を先に決める**:
   - 入力: 既存テストのケースに加えて、言語差を突くコーパス (下) と、実際の codex の `events.jsonl` を数本
   - 合格条件: rc とログに追記する文字列は完全一致。`meta.json` は「構造と値が一致」を必須にし、仕様として変えてよい差 (エラー文面など) を先に列挙して、それ以外の差は 0
3. 呼び出し元を Go 版へ切り替え、テストを Go (`go test`) に移す
4. 旧の Python を消す。`src/README.md` の一覧と、その道具の入口のドキュメント (skill / README) を同じ変更で直す
   (`new-tool-requires-entrypoint-docs.md`)
   Go の package には doc コメントが必須で、`scripts/go_packages_index.sh` で `src/PACKAGES.md` を生成し直す (issue 672 で入った。`make test-lint` が検査する)

**codex-events で Python と Go の差が出る点** (コーパスに入れる。2026-10-08 のレビューで洗い出した):

- キーの順序: Python の dict は入力順を保ち、Go の `map[string]any` は辞書順になる。`errors` / `usage` / `commands` は codex が出した任意の JSON をそのまま出し直すので、`json.RawMessage` で素通しするか、順序を保つデコードが要る
- 数値: Go の `any` は float64 になり、大きな整数の桁が落ちる。`1.0` は `1` になる → `json.Number` (`UseNumber`)
- null と欠落: `event.get("usage", {})` は欠落なら `{}`、`"usage": null` なら None。構造体へのデコードではこの区別が消える
- エスケープ: `ensure_ascii=False` に当たるのは `SetEscapeHTML(false)`。ただし Go は U+2028 / U+2029 を常にエスケープする
- 行の分割: Python の `splitlines()` は `\x0b` `\x0c` `\x1c`-`\x1e` `\x85` U+2028 / U+2029 でも割る。Go の `\n` 分割や `bufio.Scanner` は割らないので、`line_no` と `parse_errors` の件数がずれる。`bufio.Scanner` は既定で 64KB を超える行で失敗する
- エラー文面: `parse_errors[].error` と `read_error` は Python の例外の文面。Go では再現できないので、仕様の変更として扱う
- NaN / Infinity (Python は受け、Go は拒む)・重複キー・深いネスト (Python は `RecursionError` で落ち、`meta.json` を書かずに rc=1)
- 読めない / UTF-8 でない入力: Python は `read_error` の経路 (`{"complete": false, "read_error": …}` だけで rc=1) に落ちる。response 側の読み込みの失敗も同じ経路

### tmux-toast (置き換えではなく挙動の変更)

- 幅: Python は `east_asian_width in "WF"` を 2 セルと数える。`src/tuikit/termwidth` に寄せると、絵文字・結合文字・ambiguous の扱いが変わりうる。旧の幅と比べて、差を仕様として受けるかを決める
- tick 数: `round($duration / 0.05)` は Python の偶数丸め。Go の `math.Round` とは x.5 で結果が違う
- tuikit は bubbletea を引く module で、`tests/scripts/test_tuikit_consumers_aligned.sh` (issue 603) の対象になる
- 🚨 toast は hook の `run-shell` から呼ばれ、0 以外で終わると view-mode を積む (`bin/tmux-toast` の python3 の分岐の上のコメント)。
  **python3 不在の縮退は消えるのではなく、go 不在・ビルド失敗の縮退に置き換わる**。必ず exit 0 で縮退することを、`tests/tmux/test_tmux_toast.sh` の該当ケースを直して固定する

### どれから移すか

基準は「**壊れると困る順**、かつ**呼び出し元が他にもあるもの**」。行数の大きさでは決めない。

1. `codex-events.py` + `codex-fanout` の python3 — codex-fanout の結果の判定 (complete / incomplete) を担う。移すと codex-fanout の python3 依存がなくなる。
   判定を担うので、上の突き合わせを省かない
2. `zshlib/_concat_helpers.zsh` の mp4 のサイズ計算 — 実行時の本体コード
3. `tmux-toast` の幅計算 — `termwidth` があるので、自前で持つ必要がなくなる
4. 残り (`repair_avi_vorbis_audio.sh` / lockman の A-B) は触る機会が来たときに

## 受け入れ条件

- [ ] 置き場所 (A / B / C) を決め、その理由を本 issue に書く
- [ ] `codex-events.py` を Go に置き換え、上のコーパスと実際の `events.jsonl` で旧版と突き合わせて、合格条件を満たす
- [ ] `tests/test_codex_events.py` の全ケース (`EventTests` / `DriverTests`) を Go のテストへ移し、`make test-go` と CI の lane のログにテスト名が出ることを確認する。
      `DriverTests` が偽の codex を python3 の shebang で書いている点も置き換える
- [ ] `bin/codex-fanout` から `python3` の呼び出しがなくなり (`grep -n python3 bin/codex-fanout` が 0 件)、Go のバイナリは起動時に解決される
- [ ] `zshlib/_concat_helpers.zsh` の mp4 のサイズ計算を置き換える
- [ ] `tmux-toast` の幅計算を `termwidth` に寄せ、旧との幅の差を確認し、go が無いときも exit 0 で縮退することをテストで固定する
- [ ] 残りの対象 (`repair_avi_vorbis_audio.sh` / `ab_abandoned.sh` の heredoc) は、それぞれ移したか、移さない理由を本 issue に書いた

## 関連ファイル

- `bin/lib/codex-events.py` / `tests/test_codex_events.py` / `tests/claude/test_codex_events.sh` / `bin/codex-fanout`
- `zshlib/_concat_helpers.zsh` / `bin/tmux-toast` / `tests/tmux/test_tmux_toast.sh` / `src/tuikit/termwidth`
- `bin/lib/go_autobuild.zsh` (`--pkg`) / `bin/lib/runtimeout.sh` / `src/README.md`

## 進捗

- 2026-10-08: 起票
- 2026-10-08: 反証レビュー (Claude のサブエージェント 2 本、事実 / 方針。codex は 5h 枠が 91% だったため使わなかった)。
  採用 12 件: 棚卸しの漏れ 2 件 (`zshlib/_concat_helpers.zsh:397` / `src/lockman/ab_abandoned.sh:37`)、件数の誤り 2 件
  (bin の shell 35 → 33、scripts 56 → 直下の `.sh` 53)、tests のインライン python3 の列挙・`test_tmux_toast.sh` の位置付け (2 件)、
  JSON の言語差の列挙と合格条件、呼び出し経路 (起動時の解決・事前確認・stderr・GOCACHE)、置き場所の案の比較 (指紋と CI の範囲)、
  tmux-toast が挙動の変更であること (幅・偶数丸め・tuikit の検査・縮退)、受け入れ条件 (テストの移植と CI での実行の確認・`DriverTests`)、
  codex-events の commit が origin に未 push である前提。
  反証できなかった主張: 各ファイルの行数、`mutation_check.py` の参照 0 件、Python の lint の配線が無いこと、
  ルール名と issue 408 / 580 の実在、`--pkg` の前例、psd_faces.py / サンプル / `py_compile` を対象外にした判断
- 2026-10-08: ユーザーの方針で「特定のツールのためだけにあり、単体で完結する .py」を Go にしない側へ移した (`repair-avcc-avi` / `mutation_check.py` / `make_terminal_fixtures.py` を対象から外し、psd_faces.py とサンプル 9 本と合わせて表にした)
- 2026-10-08: issue 672 (package の doc コメント必須と src/PACKAGES.md) / 671 (gopls で module 横断) が完了。移植した道具もこの 2 つに乗る
