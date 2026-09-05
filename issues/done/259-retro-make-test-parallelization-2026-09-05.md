# retro: make test の高速化 (490s → 190s) — 2026-09-05

起票日: 2026-09-05
カテゴリ: retro

対象の成果: `59f9e48c` (test-discovered の腕分割 / test_go_autobuild の壁時計待ち除去 /
run_go_projects の go test 並列化)。

## 気づき

### 1. ボトルネックは「未適用の既存最適化」だった (切り出し先: 却下、記録のみ)

CI は 2026-07-20 に heavy 群を `run_tests_parallel` で並列化して 338s → 数十秒にしていたのに、
ローカルの `test-discovered` は `run_tests` の直列のままだった。**機構は既にあり、配線だけが
無かった**。新しい仕組みを足す前に「同じ問題を既に解いた経路が repo 内に無いか」を見るのは
`adversarial-review-own-safeguards.md` の 0-B (既に答えを出している経路を使う) と同じ形で、
今回はそれが効いた。新ルールは要らない。

### 2. 計測を最初から内訳が取れる形で回さず、全体を 2 回走らせた (切り出し先: 既存ルールへ追記)

最初にレーン単位 (test-lint / test-runtime / test-src) で 8 分回し、そのあと内訳が要ると分かって
テスト 1 本ずつの計測で更に 6 分回した。**最初から 1 本ずつ計測していれば 1 回で両方得られた**
(レーン合計は per-test の合計から出せる)。

- 切り出し先案: `perf-claims-need-measurement.md` に「**最初の計測は、後から内訳を聞かれても
  答えられる粒度で取る**。粗い粒度で 1 回測ると、ほぼ必ず細かい粒度で測り直すことになる」を追記
- 発動点は既存ルールと同じ (性能が主題の変更) なので新規ルールは立てない

### 3. 完了待ちのポーリングでターンを大量に消費した (切り出し先: 新規 issue か却下。要判断)

background で回した計測の完了を待つあいだ、`grep -c '[run]'` を **数十回**叩いた。
`Monitor` と `run_in_background` の until ループを使った後もポーリングを続けており、
待ち方の切り替えができていなかった。実害はトークンと応答の水増し (ユーザーから見ると
「進行中です」の繰り返し)。

- 判断待ち: これは repo のルールではなくハーネスの使い方なので、`_claude/rules/` に置くのが
  適切かどうか。置くなら「**待つと決めたら待つ**。until ループか Monitor を張った後に
  ポーリングを重ねない」

### 4. サブエージェントの報告は検閲で 2 件崩れた (切り出し先: 却下、記録のみ)

`subagent-model-tiering.md` の検閲が実際に効いた事例として記録する。

| 報告 | 検証結果 |
|---|---|
| golangci-lint が `exit 3` で落ちる | **make 経由の実測は rc=2**。tool 自身の終了コードは make に隠れて未分離 |
| `make test-discovered` = 164s | 自分の実測は **128s**。別サンプル。doc には自分で測った値を書いた |

どちらも「嘘」ではなく粒度と条件の違いだが、**doc と issue に書く数字は自分で測ったものにする**
という運用が正しかった。既存ルールで足りているので追記しない。

## 残課題

決着 (2026-09-05, dotfiles-c6):

- [x] **並列腕と直列腕が「同時に」走るケース** — 発生しない構造にした。`run_all_targets` は逐次の
      まま残し、test-lint だけが opt-in の並列ランナー (`scripts/run_make_targets_parallel.sh`) を
      使う (27df01a0)。理由は Makefile の test-lint 直上コメントに残した。
      ただし**並列化が負荷経由で既存の競合テストを壊す**別筋は残る: `tests/claude/test_claude_links_sync.sh`
      が `make test` 内で flaky になった (単独 3/3 緑、負荷下で落ちる)。これは issue 260 (dotfiles-a2 が持つ)
- [x] **孤児 worktree `wt-138` / `wt-fix138`** — ユーザーの指示で削除した。削除前に両 commit
      (aaf6c1d7 / 415eaadf) が origin/master に含まれること・`git status` が空であることを再確認
- [x] 項目 2 → `perf-claims-need-measurement.md` に「最初の計測は、後から内訳を聞かれても答えられる
      粒度で取る」を追記 (実例は rules-rationale へ)
- [x] 項目 3 → 新規ルールは立てず `verify-execution-not-just-exit-code.md` の「非同期・background の
      完了」節へ「待つと決めたら待つ」を追記 (発動点 = background の完了待ち、が既存節と同じ)
- [x] issue 258 → 対応済み (同 issue の「対応」節)
