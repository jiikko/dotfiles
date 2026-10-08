# 670 (refactor): 内部で使う Python スクリプトを Go に置き換える

起票日: 2026-10-08

## 概要

dotfiles の中で道具が内部で呼ぶ Python (`bin/lib/codex-events.py` など) を、順に Go へ置き換える。
ユーザーの方針 (2026-10-08): 「指定したファイル以外にも、内部で使うスクリプトも Go で置き換えていきたい」。

## なぜ Go か

1 本ずつ見ると Python のままで困っていないものが多い (`codex-events.py` は 84 行で、標準ライブラリしか使わず、
codex の run の後に 1 回呼ばれるだけ)。それでも寄せる理由は、言語の書きやすさではなく**検査の配線**にある:

- `src/*` の Go には lint / test / CI の lane が揃っている (`src/README.md` の 3 点セット、root の `make test-go`)
- Python には lint (ruff / mypy 等) の配線が無い (Makefile と `.github/workflows/` を grep して 0 件。2026-10-08)。
  テストは `tests/**/test_*.sh` から `python3` を呼ぶ形でだけ走る (例: `tests/claude/test_codex_events.sh`)
- LLM が書く・直す前提では、型とコンパイルが typo・型の取り違えを実行前に止める方が安い
  (`codex-events.py` が `isinstance(..., dict)` を手で書いているのは、型が無い分を手で補っている形)
- 同じ判定を Go 側からも読みたくなったとき (glogx / ratelimit が codex の run の結果を読む等)、2 言語で同じ解析を持たずに済む

## 棚卸し (2026-10-08、`git ls-files '*.py'` + 拡張子なしで shebang が python のもの + `python3` をインラインで呼ぶ箇所)

### 置き換えの対象

| ファイル | 行数 | 呼び出し元 | 備考 |
|---|---|---|---|
| `bin/lib/codex-events.py` | 84 | `bin/codex-fanout` (`event_inspector`) | **最初の 1 本**。テストは `tests/test_codex_events.py` (174 行、`tests/claude/test_codex_events.sh` 経由) |
| `bin/codex-fanout` の `python3 -c` (schema が JSON object かの検査) | 1 | 同ファイル | codex-events と一緒に外さないと、codex-fanout の python3 依存が残る |
| `bin/tmux-toast` の `python3 -c` (表示セル幅、tick 数) | 2 か所 | 同ファイル | 幅は `src/tuikit/termwidth` が既にある。python3 が無い環境では「全文字 2 セル」で代用している |
| `bin/repair_avi_vorbis_audio.sh` の `python3 -` (Ogg の組み直し) | heredoc 1 本 | 同ファイル | |
| `bin/repair-avcc-avi` | 706 | Finder メニュー (`@DotfilesSyncer`) | 拡張子なしの Python。一番大きいので後回し |
| `tests/setup/lib/make_terminal_fixtures.py` | 96 | `tests/setup/test_terminal_profile_{appearance,restore}.sh` | テストの fixture を生成する |
| `src/chromecookie/mutation_check.py` | 208 | 手動で実行 (参照 0 件) | `bin/mutate-verify` / `mutate-verify-list` (issue 408 / 580) に寄せられないかを先に見る。寄せられるなら Go にせず消す |

### 対象外 (理由つき)

- `src/pro-con/samples/*/` の Python 9 本 — 見た目を決めるための**使い捨てのサンプルレンダラ** (`decide-layout-in-sample-renderer-first.md`)。
  道具として呼ばれるものではない
- `_claude/skills/zundamon-kaisetsu/scripts/psd_faces.py` (109 行) — `psd-tools` (`uv run --with psd-tools`) に依存している。
  PSD を読む Go のライブラリで同じ層の合成ができると確かめられるまでは移さない。
  再評価の trigger: PSD の読み込みで不具合が出たとき / Go 側 (`src/zundamon-kaisetsu`) が PSD を直接読みたくなったとき
- `bin/mutate-verify` の `python3 -m py_compile` — 変異させる**相手が** Python のときの構文検査。Python が残る限り必要
- `.github/workflows/doctor.yml` の `python3 -c` / `python3 -` (出力 JSON の検査) — CI の step の中だけで使っている。
  Go の main の出力を `jq` で見る形にできるなら一緒に直してよい (優先度は低い)
- `tests/` の各 `.sh` がインラインで書いている `python3` (`bench_stats.sh` / `test_tmux_toast.sh` ほか) — テスト側の補助。
  置き換えた本体のテストを直すときに、ついでに外せるものだけ外す
- **shell スクリプト** (`bin/` 35 本 / `scripts/` 56 本) — この issue では扱わない。Go にする基準は下の「どれから移すか」と同じで、
  当てはまるものが出たら別 issue にする

## 対応方針

### 置き場所 (未決。最初の 1 本で決める)

- 推奨: 小さい道具を集める **module を 1 つ作り、道具ごとに `cmd/<name>` を置く**。`bin/<name>` は
  `go_autobuild_exec --pkg cmd/<name>` で呼ぶ (`src/doctor/cmd/svcdoctor` が前例)。
  道具 1 本ごとに `src/<name>` を作ると、84 行のために 3 点セット (Makefile・`go.mod`/`go.sum`・`src_<name>.yml`) が 1 組ずつ増える
- 呼び出し元の `bin/` が shell で、`$script_dir/lib/…` を絶対パスで解決している点 (`bin/codex-fanout`) は引き継ぐ
  (PATH 先頭の shim と同じ名前を相対名で呼ばない。`path-shim-must-resolve-real-binary.md`)

### 移し方 (1 本ごと)

1. 旧の Python を**正解役**として残したまま Go 版を書く (`adversarial-review-own-safeguards.md` 0-B)
2. 既存テストの入力 (`tests/test_codex_events.py` の各ケース) を両方に通し、出力 (`meta.json` / 追記されるログ / rc) を突き合わせる。
   食い違いが 0 になるまで旧版を消さない
3. 呼び出し元を Go 版へ切り替え、テストを Go (`go test`) か既存の `tests/**/test_*.sh` に移す
4. 旧の Python を消す。`src/README.md` の一覧と、その道具の入口のドキュメント (skill / README) を同じ変更で直す
   (`new-tool-requires-entrypoint-docs.md`)

### どれから移すか

基準は「**壊れると困る順**、かつ**呼び出し元が他にもあるもの**」。行数の大きさでは決めない。

1. `codex-events.py` + `codex-fanout` の schema 検査 — codex-fanout の結果の判定 (complete / incomplete) を担う。
   移すと codex-fanout の python3 依存が無くなる
2. `tmux-toast` の幅計算 — `termwidth` があるので、自前で持つ必要が無くなる
3. 残り (repair 系 / fixture 生成 / mutation_check) は触る機会が来たときに

## 受け入れ条件

- [ ] 置き場所 (module の形) を決め、その理由を本 issue に書く
- [ ] `codex-events.py` を Go に置き換え、旧版との突き合わせで食い違いが 0 であることを確認する
- [ ] `bin/codex-fanout` から `python3` の呼び出しが無くなる (`grep -n python3 bin/codex-fanout` が 0 件)
- [ ] `tmux-toast` の幅計算を `termwidth` に寄せる (python3 が無いときの代用の分岐も消える)
- [ ] 残りの対象は、それぞれ移したか、移さない理由を本 issue に書いた

## 関連ファイル

- `bin/lib/codex-events.py` / `tests/test_codex_events.py` / `tests/claude/test_codex_events.sh` / `bin/codex-fanout`
- `bin/tmux-toast` / `src/tuikit/termwidth`
- `bin/lib/go_autobuild.zsh` (`--pkg`) / `src/README.md`

## 進捗

- 2026-10-08: 起票。棚卸しは上の表のとおり
