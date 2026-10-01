# 586 (feat): restartable — 前面で動かすコマンドを R で再起動・Q で終了できるランナー (他 repo から go install で使う)

> 🚨 **担当中: SFTP issue checkpoint [59ecdd]**（2026-09-30〜）

起票日: 2026-09-30

## 概要

obaket の `make dev-fg-loop` (`apps/obaket/macOS/bin/dev-fg-loop`) は、アプリをビルドして前面で起動し、Claude が `bin/dev-restart` から
再ビルド + 再起動できるようにしたループ。人がキーで再起動・終了できない。ユーザーの依頼 (2026-09-30):

> コマンドのforegroupプロセスでrestartする文字を受け付けてほしい. Rを押したら、そのばでrestartして欲しい. それとQでプロセスを終了して欲しい.
> 行末に常に選択肢を表示をして、ログは今まで通りtailする感じにして。

同じことを他の repo でもやりたいので、dotfiles の `src/` に汎用の Go のツールとして作り、各 repo は GitHub の URL から `go install` して使う
(ユーザー判断: 依存が増えてよい / GitHub の URL で参照する / bubbletea と `tuikit/confirm` を使う / ログは 0.5 秒程度バッファしてよい)。

## 要件

- R1 起動: `restartable [flags] [--build <cmd>] -- <run cmd...>`。ビルドが成功したら run を起動する。ビルドに失敗したら起動せず (古い成果物を起動しない) 待つ
- R2 最下行に常に選択肢と状態を出す。見た目 (ユーザー合意): `── [R] 再起動  [Q] 終了 ──────────────── running (pid 12345)`。状態は building / running / build-failed / stopping
- R3 R を押したら確認ダイアログ (`tuikit/confirm`) を出し、y で「止める → ビルド → 起動」。ビルド中の R は無視して「ビルド中」と出す。build-failed のときの R は再ビルド
- R4 Q を押したら確認ダイアログを出し、y で子を止めて終了 (rc 0)
- R5 Ctrl-C は子を止めて終了 (rc 130)。どの終わり方でも端末の状態を元に戻す
- R6 子の stdout / stderr は最下行の上へ流す。最大 0.5 秒ほどまとめて流してよい (大量のログで遅くしない)
- R7 子が自分で終わった (人の Cmd+Q 等) ときは、再起動の要求が無ければ終了する (今の dev-fg-loop と同じ)
- R8 外からの再起動: `restartable restart --control <socket>` で、動いているインスタンスに R と同じ要求を送る (確認ダイアログは出さない)。
  `restartable status --control <socket>` で識別子・状態・子の pid・世代を返す。obaket の `bin/dev-restart` はこれを使う
- R9 止め方: `--stop-cmd <cmd>` (obaket なら debug API の quit) → 猶予 → 子のプロセスグループへ SIGTERM → 猶予 → SIGKILL
- R10 `--id-env <NAME>`: インスタンスの識別子を子の環境変数に渡す (obaket の `devLoop` の照合 = 別のアプリを止めない仕組みを残す)
- R11 stdin が端末でないとき (テスト・CI) はキー入力と最下行を出さず、ログを素通しする。control socket は同じく効く
- R12 `go install github.com/jiikko/dotfiles/src/restartable@<版>` が dotfiles の checkout 無しで通る (module path を GitHub の URL にし、`replace` に頼らない)

## 受け入れ条件

- [x] M1: `termsafe` と `tuikit` の module path を `github.com/jiikko/dotfiles/src/termsafe` / `.../src/tuikit` に変え、取り込んでいる module
      (doctor / glogx / pro-con / ratelimit / schedkeys) の import と `replace` を追従させる。全 module の `make lint` / `make test` が通り、`bin/` のラッパーがビルドできる
- [x] M2: `src/restartable` を 3 点セット (Makefile の lint / test、go.mod、`.github/workflows/src_restartable.yml`) と README 付きで足す。R1〜R11 をテストで固定する
      (キーと確認ダイアログは model の Update を直接叩く、プロセスの止め方と control socket は実プロセスで)
- [ ] M3: push 後、空の GOPATH / GOMODCACHE で `go install github.com/jiikko/dotfiles/src/restartable@<commit>` が通る (R12)
- [ ] 見た目 (最下行と確認ダイアログ) と R / Q の操作を、ユーザーが実端末で確かめる ([587](587-human-verify-restartable-ui-and-keys.md))
- [x] obaket 側の切り替え (dev-fg-loop / dev-restart) は obaket に別の issue を起こす (obaket issue 1007。実装は obaket の worktree で済み、版の固定待ち)

## 進捗

- 2026-09-30 起票
- 2026-10-01 M1 完了
  - module / import / require / replace と package path の説明を GitHub path に統一。各 `replace` は従来どおり `../termsafe` / `../tuikit` を参照。
  - 7 module の `go mod tidy` はすべて成功。7 module それぞれの `make lint` / `make test` は最終実行で rc=0。
  - 指定の旧 path grep は 0 件（grep rc=1）。`GO_AUTOBUILD_SYNC=1 bin/glogx --help` は再ビルド後に rc=0。`pro-con help` / `schedkeys --help` / `svcdoctor --help` も rc=0。
  - codex は `doctor/internal/displaycheck` に termsafe の別名解決を足したが、Claude が使い捨ての worktree で元の版に戻して doctor の test を回すと rc 0 だった
    (package 名は termsafe のまま) ので戻した (commit「revert(doctor): displaycheck の termsafe 別名の解決を戻す」)。glogx の初回 race test は時間・allocation 閾値の失敗が出たが、単独再実行では rc=0 (codex の報告)
  - Claude の確認: 7 module の `make lint` / `make test` を回し直して全部 rc 0 (各 test ログに ok 行があり FAIL 0)、旧 import path の grep 0 件
  - commit「refactor(src): termsafe と tuikit の module path を github.com/jiikko/dotfiles/src/... にする (issue 586 M1)」
  - ratelimit の `--help` は usage を表示して rc=2（現在の `run` が flag parse error を usage code に写す）。
- 2026-10-01 M2 (中核・端末の UI・README・CI の workflow) 完了。codex-drive: 設計案 (codex) → 設計の敵対レビュー (codex。P1 2 / P2 4 を設計 v2 に反映) → 実装 (codex) → 検閲 (Claude) → 敵対レビュー 5 周
  - Claude の検閲で直したもの: 停止の待ち中の Q で runner だけが終わり、終了時の後始末が生きているアプリを SIGKILL していた / Esc の取り消しが再起動の停止にしか効かない ほか 2 件
  - 敵対レビュー 1 周目 (opus): ビルドの二重起動・強制終了の取りこぼし・Cmd+Q の再起動化 (Claude の前回の修正指示の誤りを design-v2 の E に戻した) ほか、10 件を直した
  - 2 周目 (opus): 最下行がまったく描かれない (1 周目の SIGPIPE 対策で出力を包み、bubbletea の端末判定が外れた。本物の pty のテストを足した)・キーあふれのハング・猶予の早抜け・SIGPIPE の継承・子の stdin
  - 3 周目 (opus): 端末の stdin が headless 側の子に渡る・観測していなかった pty テスト・ログの順序・CR・0x0 の端末
  - 4 周目 (codex 3 本並列): 確認中の Cmd+Q・操作キーの破棄・通常の終了で孫を SIGKILL・出力先の失敗・status の古さ (5 件とも直す前のテストで再現してから直した)
  - 5 周目 (codex 1 本) と最終確認: Ctrl-C を保留の列より優先・保留に上限。全体のテスト中に見つけた `panic: send on closed channel` (control の Close と接続の競合) を直した
  - 周回を止めた理由: 指摘が周ごとに小さくなり (P1 は 3 周目から無し)、最終確認の P2 2 件は採らない / 記録とした (下記)
  - 記録のみ (コードのコメントと README): 保留が 16 を超えると送信中の y の後ろの Esc が捨てられうる / Ctrl-C は既にキーのチャネルに入ったキーを追い越さない /
    改行を送らずに接続を保つ相手の goroutine は相手の切断まで残る / 同じ checkout でも --control を別にすれば runner を 2 つ起動できる (設計どおり) /
    ビルド中の Q は前世代の子孫のグループまで撃ちうる / 回収済み leader の pgid の再利用
  - 未解決の観測: 4 周目の修正の直後に全体の `go test -race` が 2 回 rc 1 だったが、Claude がログを消していて落ちたテストを特定できていない。その後は 20 回以上連続で rc 0
    (2 本同時に走らせる負荷の下を含む)。再発したら、落ちたテスト名をここに書く
  - 検証 (Claude): make lint rc 0、go test -count=1 -race 5 回連続 rc 0 (77 本、skip 0。本物の pty のテスト 3 本を含む)、scripts/check_go_project_lanes.sh rc 0、
    python の pty (80x24) で起動して最下行・running (pid N)・Q の確認ダイアログが出ることを目視
