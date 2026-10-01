# 610 (bug): bin/ のスクリプトが失敗を成功に見せる・不正な値を ffmpeg に渡す (concat_movies / karabiner / ci-log)

起票日: 2026-10-02

## 概要

bin/ のシェルスクリプトを安定性の観点で監査した (sonnet の read-only 監査 → 主要な指摘を main で再現して裏取り)。
直すものと、記録だけにするものを分ける。

## 直すもの

### 1. `bin/concat_movies`: `bc` が 1 未満の値の先頭の 0 を落とし、ffmpeg が fade を拒否する

- `fade_st=$(echo "$x - $fadeout" | bc)` は `1 - 0.3` に対して `.7` を返す (実測)。ffmpeg は
  `Unable to parse "st" option value ".7"` で落ち、`set -e` でスクリプトが止まる
- 発火: `--thumbnail-duration 1 --fadeout 0.3`、または 1 秒未満のクリップに `--fadeout`
- 副次: ffprobe の duration が `N/A` (mkv 等で stream に duration が無い) のとき、`echo "N/A - 0.7" | bc` は
  **黙って 1.3 を返す** (実測。bc が N と A を 16 進の桁として読む)。fade の開始位置が嘘のまま変換が進む
- 直し方: 引き算を awk で行い、入力が数値でなければエラーで止める (1 つの関数に寄せる)

### 2. `bin/restore_karabiner_config.sh` / `bin/backup_karabiner_config.sh`: 失敗しても成功の文言を出して rc=0

- `jq ... > tmp && mv tmp target` が失敗しても、次の行の「(jis に自動設定)」が出て rc=0。0 バイトの `karabiner.json.tmp` が残る
  (監査で再現: 壊れた JSON + JIS を返す ioreg のスタブ)
- 冒頭の `cp "$src" "$target"` も未検査で、失敗しても古い sha を「restore 済み」として記録する。backup 側の `cp` も同じ
- 直し方: 失敗した段で `.tmp` を消して非 0 で止める

### 3. `bin/ci-log`: `gh` が失敗すると「失敗した run はありません」と出る

- `mapfile -t failed < <(gh run list ...)` はプロセス置換なので gh の rc が見えない。認証切れ・ネットワーク断で
  **「HEAD に失敗した run はありません」** を出し、その分岐の末尾の明示の `exit 0` で **rc=0** で終わる
  (監査で再現、反証レビューで rc を確認)。認証切れでも「CI は緑」と読める
- 同じ分岐の「HEAD より前の赤」の走査 (`done < <(gh run list ...)`) も gh の失敗を見ない
- `mapfile` は bash 3.2 に無い (shebang は `env bash`。PATH に Homebrew bash が無い環境で落ちる)
- 直し方: 2 か所の gh の出力を変数へ取って rc を見る。`mapfile` を `while read` にする

## 記録だけにするもの (今回は直さない)

| 箇所 | 内容 | 直さない理由 / trigger |
|---|---|---|
| `bin/mutate-verify` | `--verify` の run に timeout が無い。変異で hang すると止まらない (mutate-verify-list も詰まる) | 変異検証の判定 (第 3 の結果) に関わる安全機構の変更で、単独の作業として敵対レビュー込みで行うべき。trigger: hang する変異に実際に当たったとき |
| `bin/codex-fanout` | watchdog が TERM を 1 回送るだけで KILL へ昇格しない | 推測 (未再現)。codex を使う作業でのみ発火。trigger: codex-fanout が timeout 後も返らなかったとき |
| `bin/concat_movies` | 音声の無いクリップを混ぜると、音声の欠けた連結を「完了」として出す | 実害 (音ズレの程度) が未検証。trigger: 音声なしの素材を連結する用途が出たとき |
| `bin/repair_avi_vorbis_audio.sh` / `bin/repair-avcc-avi` | `ffmpeg -y` で既定の出力名を無警告で上書きし、途中失敗で壊れた出力が残る | 修復ツールの出力は都度確認して使う運用。trigger: 上書きで成果物を失った実例が出たとき |
| `bin/lgtm.sh` | 元画像を一時ファイルなしで上書きする | 使い捨ての画像加工で、元画像は通常別に残っている |
| `bin/skill-eval` | `claude plugin eval` に壁時計の上限が無い (費用上限のみ) | hang するかは未確認 |

監査で問題無しとしたもの: go_autobuild.zsh (起動時の指紋は glogx で約 7 ms、pro-con で約 14 ms。どちらも再帰 glob が律速で、
安全側の設計 (mtime の順序比較に戻さない) を崩さずに削れる幅が小さいので見送り) ほか。
動画系の本体 (`zshlib/*.zsh`) はこの監査の範囲外。

## 受け入れ条件

- [x] 1〜3 を直し、それぞれの失敗を再現する入力で直ったことを確かめる (結果を下に)
- [x] concat_movies の引き算の関数に回帰テストを足し、bc に戻す変異で red を見る

## 関連ファイル

- `bin/concat_movies` / `bin/restore_karabiner_config.sh` / `bin/backup_karabiner_config.sh` / `bin/ci-log`

## 進捗

- 2026-10-02 「fix(bin): concat_movies の秒の引き算・karabiner の失敗・ci-log の gh の失敗を成功に見せない (610)」
  - 1: `sub_seconds` (数値の検査 + awk の `%.3f`) に寄せ、bc を依存から外した。stream の duration が `N/A` / 空なら
    `format=duration` へ落とす。フェードアウトより短い長さは非 0 で止める (以前は負の st を ffmpeg に渡していた。ffmpeg が受けたかは未実測)
  - 2: restore は cp / jq / mv / 記録のどれが失敗しても非 0 で止め、`.tmp` を消す。「restore 済み」の記録は全段が成功した後だけに移した。
    backup は cp と記録の失敗で止める
  - 3: 2 か所の `gh run list` の出力を変数へ取り、失敗なら「CI の状態は分かりません」で rc=1。`mapfile` を `while read` にした
  - テスト (どれも偽の ffmpeg / ffprobe / gh / jq、HOME の差し替え): `tests/bin/test_concat_movies.sh` 6 件 /
    `test_karabiner_config_scripts.sh` 8 件 / `test_ci_log.sh` 10 件 (bash 5 と /bin/bash 3.2 の両方)
  - 変異 (mutate-verify、すべて想定の検査で red): bc に戻す → 「1 未満の開始位置」/ N/A の落とし先を外す → 「N/A の落とし先」/
    restore の jq 失敗時の exit を `:` に → 「jq の失敗」/ backup の cp の検査を外す → 「backup のコピーの失敗」/
    ci-log を旧実装に戻す → 「1 本目の gh の失敗」/ 2 本目の `|| gh_failed` を外す → 「2 本目」/ restore の記録の失敗の exit を `:` に → 「記録の失敗」
  - 敵対レビュー (sonnet、read-only、1 周): P2 1 件 = restore が記録の失敗 (mkdir / 書き込み) を握り潰して rc=0 (再現あり) → 直した (変異で red を確認)。
    P3: CR 付きの長さ (`5.0\r`) を拒否する = macOS の ffprobe は CR を出さないので記録のみ / フェードアウトより短いと止まる = 意図どおり /
    ci-log の HEAD より前の赤の走査が未検査 = テストを足した。2 周目は §7 の例外 (判定ロジックを新設せず、各修正を変異で直接確認) で打ち切り
  - 残り: 上の「記録だけにするもの」の表 (mutate-verify の timeout ほか)。`ci-log <run-id>` の経路も `gh run view ... 2>/dev/null || true` で
    gh の失敗を「失敗した job はありません」にしうるが、成功した run に `--log-failed` を当てたときの gh の rc を実測していないので触っていない
  - `make test` (worktree): 新しい 3 本と tmux shim の 2 本は [ok]。全体は rc=2 で、落ちたのは今回の変更と無関係な 2 本:
    test-yaml (`src/tuikit/.golangci.yml:56` の line-length。2fa1692e で入った。持ち主のセッションへ連絡済み) と
    `tests/claude/test_deny_piped_push_then_destroy.sh` (9MB 入力で timeout rc=124。ロードアベレージ 27 の最中だった。単独の再実行は rc=0)
