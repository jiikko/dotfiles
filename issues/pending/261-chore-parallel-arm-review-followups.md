# test-discovered の 2 腕分割で残った敵対レビュー指摘 (P2 4 件)

起票日: 2026-09-05
カテゴリ: chore
優先度: 低 (P2-3 は対応済み。残る P2-4 は trigger 待ちなので pending)
状態: **pending** (2026-09-05 ユーザー判断)

**着手条件 (trigger)**: 並列腕 (`test-discovered-parallel`) で tmux の本番サーバ、または `tmp` の固定名への
書き込みに起因する flaky / 干渉が出たとき。現時点で違反はゼロ (下記 P2-4) で、入れ忘れは並列実行の
干渉として表に出るため、先回りの検査は作らない。着手するなら動的 shim 案 (効果で判定) を優先する。

`59f9e48c` (test-discovered の並列腕 + 直列腕への分割) に対する敵対的レビューの残り。
**P1 2 件と P2-5 は `f0732cda` で対応済み**なのでここには含めない。

## 対応状況 (2026-09-05)

- **P2-3 / P2-1 / P2-2: 対応済み**。`run_test_arms` (Makefile) が両腕の失敗テスト名と skip を合算して
  最後に出し、`tests/scripts/test_no_failfast_entrypoints.sh` が test-discovered / test-discovered-rest を
  「本物の Makefile を include した一時 Makefile で腕のレシピだけを偽のテストディレクトリに差し替える」
  形で検査する (prerequisite 形への退行 / 合算の追記外し / rc 握り潰し の変異 6 本で red を確認)。
- **P2-4: 未対応 (残す)**。下記に調査結果を追記。

## P2-3: `test_no_failfast_entrypoints.sh` が test-discovered 系を対象にしていない (対応済み)

現在の対象は `test-go-lint` / `test-go` / `test_changed.sh` だけ。**`test-discovered` /
`test-discovered-rest` / `test-runtime` を誰も検査していない**ため、

```make
test-discovered: test-discovered-parallel test-discovered-serial   # ← prerequisite 形へ戻す退行
```

が緑で通る。この形は issue 109 が禁じたもの (1 本目の失敗で残りが 1 度も走らないまま CI ログから
消える) で、**2 腕分割はこの契約に依存している** (並列腕が落ちても直列腕は走る、を
`59f9e48c` の変異検証で確認した)。守る仕組みが無いと次の改修で黙って壊れる。

やり方は既存テストと同じで、偽の腕 (必ず落ちる方が先) を差し込んで「2 つ目が走ったか」と
「失敗したターゲット名が両方出るか」を見る。`GO_PROJECT_DIRS` を差し替えている既存の手が
そのまま使えるかは要確認 (腕はターゲット名が固定なので、変数化するか別の入口が要るかもしれない)。

## P2-1: 並列腕の失敗テスト名が、直列腕 41 本の出力の上へ流れる (対応済み)

2 腕になったことで、`✗ 失敗したテスト:` (並列腕) を出した後に直列腕 41 本の実行ログが続き、
最後に出るのは `✗ 失敗したターゲット: test-discovered-parallel test-discovered-serial` の
**ターゲット名だけ**でテスト名の再掲が無い。Makefile:126-127 が自分で「1 本目の失敗で埋もれると
1 件だけ直せばよいと誤読される」と書いて避けた形に、腕の単位で戻っている。

案: 両腕の失敗一覧を共有ファイルに集め、`test-discovered` の最後に合算して出す
(`run_all_targets` は汎用なので、腕を束ねる側で持つ方が素直)。

## P2-2: `[skip] N 件` が 2 腕で別々に出て、合計が無い (対応済み)

Makefile:170-172 が「skip が**増えた**ことに気づく」ための件数表示だと書いているのに、
基準にする 1 つの数が消えた (今は 2 つの数を人が足す)。P2-1 と同じ場所で直せる。

## P2-4: `SERIAL_TEST_DIRS` に乖離検査が無い (未対応)

`CI_HEAVY_TEST_DIRS` には `test-ci-group-deps` (`scripts/check_ci_group_deps.sh`) があるが、
`SERIAL_TEST_DIRS` は手書き列挙のままで、**共有資源に触るテストを別ディレクトリに置くと黙って
並列腕へ入る**。

🚨 **現時点では違反ゼロ**: レビュワーが並列腕 70 本を全走査し、ソケット未指定の bare な tmux
呼び出し 0 件 / `tmp` 固定名への書き込み 0 件を確認済み (壊せなかった)。つまり今すぐの実害は無く、
**将来の混入を止める検査**の話。検査を書くなら「並列腕のテストが `tmux` をソケット指定なしで
呼んでいないか」など、**効果 (何を共有するか)** で判定する形にする (ディレクトリ名の一覧を
二重管理しても乖離が別の場所へ移るだけ)。

### 2026-09-05 の調査メモ (P2-4 を残した理由)

- 並列腕で実サーバの tmux を使うテストは `tests/claude/test_tmux_pane_state_bell.sh` 1 本で、全呼び出しが
  `tmux -L "$SOCKET"` (違反ゼロは再確認)。他に `tmux` の語が出るのは hook のテストベクタ
  (`tests/claude/test_deny_bare_tmux_kill.sh` の引用文字列 50 行超) と stub 方式のテスト
- 静的検査 (「`tmux` コマンド語の直後に `-L`/`-S` が無い行を弾く」) を書くと、上のテストベクタが
  そのまま偽陽性になり、`check_ci_group_deps.sh` と同じ `allow` コメントの逃げ道が要る。
  逃げ道つきの字句 gate は `adversarial-review-own-safeguards.md` §8 の形 (脅威モデルと「検出しない形」
  を先に書く) を通す必要があり、この issue の余力では収まらなかった
- 動的検査 (並列腕の実行中だけ PATH 先頭に「-L/-S 無しで呼ばれたら落ちる tmux shim」を置く) は、
  stub 方式のテストが自前で PATH 先頭に偽 tmux を置くため shim が影に入り、効果で判定できる
  範囲が実サーバ利用のテストに限られる。着手するならこちらが「効果で判定」に近いが、shim は
  `path-shim-must-resolve-real-binary.md` の規律 (絶対パス解決 / 自己参照の検出) が要る
- `_claude/hooks/deny-bare-tmux-kill.sh` のトークン走査 (引用符・コメントを除外済み) は流用候補だが、
  検出対象が kill-server / kill-session に限られ、bare な `new-session` は通す

## 参照

- `f0732cda` — P1-1 (xargs が捨てた未実行の検出) / P1-2 (実行本数の突き合わせ) / P2-5 (実測表の訂正)
- `59f9e48c` — 2 腕分割の本体
- `_claude/rules/adversarial-review-own-safeguards.md` — この一連のレビューが発動した根拠
  (自分で新設した検査を自己レビューで閉じない)。**変異検証を 5 項目通した後でも P1 が 2 件出た**
  実例として、同ルールの「変異は自分が想定した不変条件しか試さない」を裏づける
