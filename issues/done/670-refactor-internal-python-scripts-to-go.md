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

- [x] 置き場所 (A / B / C) を決め、その理由を本 issue に書く (B。下の進捗)
- [x] `codex-events.py` を Go に置き換え、上のコーパスで旧版と突き合わせて、合格条件を満たす。
      **実際の `events.jsonl` との突き合わせは未実測** (手元に 0 件。codex は自発的に起動しない方針のため取れない。下の進捗の trigger)
- [x] `tests/test_codex_events.py` の全ケース (`EventTests` / `DriverTests`) を Go のテストへ移し、`make test-go` と CI の lane のログにテスト名が出ることを確認する。
      `DriverTests` が偽の codex を python3 の shebang で書いている点も置き換える。
      CI (run 37804372152、headSha 108c5823) は `go test -race ./...` → `ok codexevents 6.470s`。CI は `-v` なしなのでテスト名は出ない。
      driver のテストまで走ったことは所要時間 (判定だけなら 0.3 秒) と、手元の `-v` で 13 本の PASS を見て確かめた
- [x] `bin/codex-fanout` から `python3` の呼び出しがなくなり (`grep -n python3 bin/codex-fanout` が 0 件)、Go のバイナリは起動時に解決される
- [x] `zshlib/_concat_helpers.zsh` の mp4 のサイズ計算を置き換える
- [x] `tmux-toast` の幅計算を `termwidth` に寄せ、旧との幅の差を確認し、go が無いときも exit 0 で縮退することをテストで固定する
- [x] 残りの対象 (`repair_avi_vorbis_audio.sh` / `ab_abandoned.sh` の heredoc) は、それぞれ移したか、移さない理由を本 issue に書いた

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
- 2026-10-09: 1 本目 (codex-events) を Go に置き換えた (commit: codex-events を Go に置き換え、codex-fanout から python3 をなくす (issue 670))

### 置き場所: B (道具ごとに `src/codexevents`)

次に移す 2 本 (mp4 のサイズ計算・toast の幅) は codex と責務が違うので、A (1 module に集める) だと無関係な編集で再ビルドが走り、
CI も全部走る。runtimeout が同じ形 (小さい CLI を 1 module) の前例。3 点セットは定型で、1 組の手間は小さかった。
C は寄せる先 (ratelimit) と責務が違う。glogx 等から判定を import したくなったら、そのとき package に切り出す (trigger)。

### やったこと

- `src/codexevents` (`codex-events inspect` / `is-object`)・`bin/codex-events` (go_autobuild のラッパー)・`src_codexevents.yml`
- codex-fanout: `-J` / `-S` のときだけ起動時に `codex_events_resolve` で解決し、`"$CODEX_EVENTS" inspect` / `is-object` を呼ぶ。python3 の事前確認を消した
- 解決の共通化: `runtimeout_resolve` と同じ処理になるので `bin/lib/go_tool.sh` の `go_tool_resolve` に寄せ、両方がそれを使う。
  🚨 書いている途中で、zsh では関数の中の `local path` が PATH を空にする (zsh の `path` は PATH と結び付いた配列) のを踏んだ。
  bash では動くので、`tests/bin/test_go_tool_resolve.sh` が bash と zsh の両方で解決できることを見る
- テスト: `EventTests` 3 本と `DriverTests` 8 本を Go へ移した (偽の codex は bash。Python の calls.jsonl は 1 行 1 引数の calls.txt に)。
  旧の 3 ファイル (`bin/lib/codex-events.py` / `tests/test_codex_events.py` / `tests/claude/test_codex_events.sh`) を消した

### 旧版との突き合わせ (正解役は Python 版)

- 入力 43 種類 × meta.json のパスの書き方 3 通り (`meta.json` / `./sub//meta.json` / `sub/./meta.json`) = 129 回で **差 0 件**。
  rc とログの追記はバイト一致、meta.json は値の一致 (Python の例外の文面 `parse_errors[].error` / `read_error` だけ除く)
- 入力: 既存テストの形 9・行の区切り (\r\n・\r・\v \f \x1c-\x1e・\x85・U+2028 / U+2029・末尾の改行・空白だけの行) 7・
  JSON の言語差 (大きな整数・1.0・-0・1E2・重複キー・キーの順・HTML の記号・エスケープ・深さ 200・オブジェクトでない行・余分なデータ・BOM・末尾のカンマ) 13・
  item / type / usage / thread_id の型の揺れ 4・応答 (空白だけ・CRLF・ファイル無し・2 つのメッセージ・メッセージの CRLF) 4・読めない入力 (events 無し・UTF-8 でない・応答がディレクトリ) 4
- ハーネスが差を見分けることの確認: 新版のログの文言を変えると差 69 件、行の区切りから \v を外すと差 3 件
- ハーネスは使い捨て (`./tmp`)。移した後は Go のテストが正解を持つ

### Python 版と変えたこと (仕様の変更として受ける)

- `NaN` / `Infinity` を含む行: 旧は値として受けて meta.json に規格外の `NaN` を書いた。新は読めない行 (parse_errors) にする (未完了の側に倒れる)
- 孤立したサロゲート (`"\ud800"`) を含む agent_message: 旧は meta.json を書いた後、ログへ書くところで例外になりログが残らない (rc=1)。新は U+FFFD にして書く
- `parse_errors[].error` と `read_error` の文面 (Python の例外の文面は再現しない)
- `-S` の schema がオブジェクトでないとき、止まるのが出力先のディレクトリを作った後になった (空の出力先が残る。空なので次の実行は拒否されない)

### 敵対レビュー (opus 1 体)

- 採用 P2: `"text": null` の agent_message が最後に来ると、旧は 1 つ前の本文で補って完了、新は未完了 (Go は null を string へ読んでもエラーにしない) →
  文字列の値だけを採るよう直し、テストとコーパス (`msg-text-null`) に足した。変異で red
- 採用 P3: `is-object` が UTF-8 でない schema を通す (Go の JSON は文字列の中の不正なバイトを U+FFFD にする) → UTF-8 を確かめてから読む。変異で red
- 記録のみ (どれも誤って「完了」にはならない):
  - JSON の上限の差: 5000 桁の整数 (旧は Python の桁数の上限で未完了、新は完了)・深さ 20000 の入れ子 (旧は完了、新は Go の上限で未完了)。実際の codex の出力では起きにくい
  - codex-fanout の中での本物の解決の経路は、テストでは偽物に差し替えている (runtimeout と同じ形)。起動の時点で止まるので気づける。レビュワーが手で 1 回通して complete を確認
  - 既にある `CODEX_EVENTS` がこの checkout のバイナリを指していれば、src が新しくても再ビルドしない (RUNTIMEOUT と同じ設計)
  - 文字列の中の生の U+0085 / U+2028 で行が割れるのは旧も新も同じ (既存の欠陥。codex はエスケープしない)
- 壊せなかったもの: 空白と行の区切りを全コードポイントで Python と比べて差 0 件・パスの表記 15 通り・50MB の行・応答のファイルの種類 9 種 (FIFO・権限なし・symlink の
  ループ等)・go_tool.sh を /bin/bash 3.2・brew の bash・zsh から `set -eu` の下で

### 検証

- `make -C src/codexevents test lint` 緑 (13 本)・`make test-lint` 緑・`tests/bin/test_go_tool_resolve.sh` 5 件
- 変異: Go 側 14 本すべて red (レビュー後の 2 本を含む) (parse_errors を見ない・本文の補い・error イベント・turn.started の巻き戻し・\r の統一・\v の区切り・
  \x1c の空白・パスの . ・usage の既定値 (最初 green → テストを足した)・UTF-8・codex-fanout の rc=65・schema の確認)。
  シェル側 3 本すべて red (`local path`・既にある値を無条件に採る・export しない)
- `make test` は 2 件落ちた。どちらもこの変更と無関係: `tests/claude/test_claude_mods.sh` の tool-elapsed の 5 秒の時間切れ
  (負荷の高い間。`make test-dir DIR=tests/claude` では通った) / `test-unused-excluding-tests` の `src/treefiler/filer/render.go:(*canvas).plain`
  (別のセッションが作業中の module)

### 残り

- **実際の codex の events.jsonl での突き合わせ (未実測)**。trigger: 次に codex-fanout を `-J` で回したとき、出力先の `*.events.jsonl` と応答を
  旧版 (`git show <この commit の親>:bin/lib/codex-events.py`) と新版の両方に通して、rc・ログ・meta.json を比べる
- 2 本目以降 (`zshlib/_concat_helpers.zsh` の mp4 のサイズ計算 / tmux-toast の幅 / 残り) は未着手
- 2026-10-09: 2 本目 (concat の mp4 の実効サイズ) を Go に置き換えた (commit: concat の mp4 の実効サイズを Go に置き換える (issue 670))

### 2 本目: `__concat_mp4_effective_size`

- 着手前の裏取り: この値は concat の出力が入力の合計より 5% 以上小さいときの診断 (`__concat_diagnose_output`) だけが使う。
  入力の末尾の「どの box からも参照されないデータ」を見分けて、出力が実効サイズどおりなら失敗 (rc=1) ではなく警告 (rc=2) にする。
  **この経路にはテストが 1 本も無かった**ので、Go に移してテストで固定する価値があった
- 置き場所: B (`src/mp4box`、`bin/mp4-effective-size`)。codex と責務が違う。glogx 等に mp4 の box を読む既存のコードは無かった
- zsh 側: `_concat_helpers.zsh` が読み込まれたときに `${${(%):-%x}:A:h:h}/bin` を控え、絶対パスで呼ぶ (関数の中の `%x` は呼び出し元を指す)。
  zshlib から Go の道具を呼ぶ最初の例
- 旧版との突き合わせ: 合成した壊れ方 17 種 (正常・末尾のごみ・64 bit の大きさ・途中で切れる・大きさ 0・型の検査・大きさ 8 未満・ファイルの外・空 等) +
  実物の mp4 2 本 + 実物の末尾に 20MB のごみ 2 本 + ディレクトリ・無いファイルの 23 件で差 0 件。型の検査を外した変異版で差 1 件 (ハーネスの空振りでない)。
  レビュワーが別に 1500 件のファズで差 0 件
- テスト: `src/mp4box/main_test.go` (13 の形) と `tests/zshrc/concat/test_concat_orphaned_data.sh` (診断の経路: 警告 rc=2 / ごみの無い入力は rc=1 /
  道具を起動できないときは警告にせず rc=1 と「測れなかった」を添える)。go が無い環境では skip (77)
- 🚨 CI: concat のテストが走る heavy のジョブは Go を入れていなかった (`needs-go: false`) → `true` にした (レビュワーが未確認とした点を確かめて見つけた)

### 2 本目の Python 版と変えたこと

- 道具を起動できない (go が無い・ビルドの失敗) とき、旧は python3 (macOS に標準) で測れていた。新は 0 を出して rc=1 にし、診断に
  「入力の実効サイズを測れなかった」を添える (警告に落とさず、失敗のまま理由を残す)
- ブロックデバイスを渡すと旧はデバイスの大きさを測るが、新は 0 (Stat の大きさが 0)。concat の入力は動画ファイルなので受ける

### 2 本目の検証

- 変異: Go 側 4 本 red (型の検査・大きさ 8 未満・64 bit の大きさ・大きさ 0)。「ディレクトリなら 0」は外しても緑 → Go では読む時点でエラーになる冗長な分岐だったので消した。
  zsh 側 3 本 red (道具の場所を 1 段ずらす・「測れなかった」の印を立てない・rc を返さない)
- 反証レビュー (sonnet 1 体): P1 なし。採用: go が無いと警告が黙って失敗に落ちる (P2) → 上の「測れなかった」。CI の Go の不足 (確かめて見つけた)。
  記録のみ: 初回のビルドは stderr を捨てて走る (診断の頻度は低い) / 数でない出力を 0 にする検査は外しても緑 (ビルドの出力は stderr にしか出ないので、
  stdout に混ざる経路は再現できなかった。予防として残す)
- `make test-dir DIR=tests/zshrc/concat` 緑・`make -C src/mp4box test lint` 緑・`make test-lint` 緑
- 2026-10-09: 3 本目 (tmux-toast) を置き換え、残り 2 件は移さないと決めた (commit: tmux-toast の幅を termwidth (Go) で測り、python3 をなくす (issue 670))

### 3 本目: tmux-toast

- 幅: `src/tuikit/cmd/termwidth` (`termwidth.Of` を出すコマンド) と `bin/termwidth` (go_autobuild の `--async`) を足し、tmux-toast から呼ぶ。
  測れない (go / zsh が無い・ビルドの失敗・数でない出力) ときは従来どおり「全文字 2 セル」の上界。
  🚨 ビルド済みのバイナリが無いときは待たずに上界で出し、ビルドは裏で起こす (初回のビルドは同期なので、前景で呼ぶ
  `scripts/tmux_paste_clipboard.sh` の通知が止まる。反証レビューの P2)
- tick (描き直しの回数): Go のバイナリにはせず awk の四捨五入にした (ただの算術で、0.05 秒の刻み。python の偶数丸めと違うのは、ちょうど .5 になる
  duration だけで差は 1 回)。旧は duration を python のコードに埋め込んでいた (`-v` で渡すので注入の余地も消えた)
- 幅の差 (全コードポイント 151,963 個を旧と新で比べた): 違うのは 1,892 個。結合・書式の文字 1,831 個 (旧 1 / 新 0。新が正しい)・制御 33 個・
  国旗の部品 26 個 (旧 1 / 新 2)。実際に流れている toast の文言 4 つは同じ幅。ZWJ でつないだ絵文字は旧 12 / 新 9 (新が実際の表示に近い)
- tuikit の depguard の「部品は純粋な層」の `**/termwidth/**` が `cmd/termwidth` にも当たったので、`!**/tuikit/cmd/**` で外した (cmd は部品でない入口)
- 検証: `tests/tmux/test_tmux_toast.sh` 28 件緑 (「あ b」= 4 セルの座標は本物の bin/termwidth を通る)・`make -C src/tuikit lint test` 緑・
  tuikit の消費者の版の検査緑・`make test-lint` 緑。変異 4 本 red (termwidth を使わない・数でない幅の縮退・tick を 0・バイナリの有無の分岐)
- 反証レビュー (sonnet 1 体): P1 なし。採用 P2 (上記の初回ビルド)。記録のみ: タブは旧 1 / 新 0 (実際は最大 8 セルで、どちらも足りない) /
  ZWJ の家族の絵文字を tmux が 1 グリフにしないと新の幅ではみ出しうる (tmux 3.7 の挙動は未確認。流れている文言には無い) / tick の回数そのものは
  テストが固定していない (0 にする変異は red) / tuikit を触るたびに次の toast が裏で再ビルドを起こす (async なので害は無い) /
  cmd/termwidth の usage の rc を固定するテストは無い

### 残り 2 件 (移さない)

- `bin/repair_avi_vorbis_audio.sh` の Ogg の組み直し: 壊れた AVI の音声を直す道具で、利用者が単体で起動して完結する (`bin/repair-avcc-avi` の案内から
  呼び分ける)。ユーザーの方針「特定のツールのためにあり、単発で完結している .py はそのまま」に当たる。再評価の trigger: 別の道具の処理の一部として呼ばれるようになったとき
- `src/lockman/ab_abandoned.sh` の main.go の書き換え: issue 362 の手動の A-B 計測ハーネスで、`make test` からは走らない。同じ方針で残す

### 閉じる時点の残り

- **実際の codex の events.jsonl での突き合わせ (未実測)**: 上の「残り」の trigger のまま (次に codex-fanout を `-J` で回したとき)
- 対象外として挙げた `.github/workflows/doctor.yml` の `python3 -c` と tests のインラインの python3 は、この issue では扱っていない (上の「その他の対象外」)
