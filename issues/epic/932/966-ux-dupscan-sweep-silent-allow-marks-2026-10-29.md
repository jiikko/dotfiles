# 966 (ux): sweep は allow 印で消した組も効かなかった印も言わない (tuikit の dogfood、2026-10-29)

> 🚨 **担当中: dotfiles-4c**（2026-10-29〜）

起票日: 2026-10-29

## 概要

dupscan-dogfood で「src/tuikit の sweep が出す組を判定して閉じる」をこなしたときの所見と、その修正。
既存の issue との重複照合はしていない (skill dupscan-dogfood の 3 節)。同じ日に別のセッションが書いた 965 (treefiler) だけは読み、F3 はそちらの F3 と同じ箇所なので一緒に直した。

## 開発タスクの結果

- `dupscan sweep src/tuikit` (単位 334・1,700 B・0.16 秒): 節 1・3・4・5 は「なし」、節 2 (下限 12 節・L1m) に 3 組
- 3 組とも寄せないと判断し、理由を実コードで確かめてから印を付けた (commit「chore(tuikit): sweep の L1m 段に出る 3 組に寄せない理由の印を付ける」)
  - `Exec.SetStdin/SetStdout/SetStderr`: bubbletea の `ExecCommand` (`charm.land/bubbletea/v2` の exec.go `type ExecCommand`) が 3 つを別の型で要求する
  - `List.Advance` / `Pager.Advance`: 相手が `anim.CursorGlide`+`Cursor` と `anim.ScrollGlide`+`Offset` で違う
  - `ScrollGlide.Offset` / `CursorGlide.Cursor`: 同じなのは読み出しの 4 行だけ。`Start` (進行中なら即時へ倒す / 捨てて始め直す) と `Advance` (整数の二次 ease-in / EaseOutBack) が違う
- 印の後の sweep は 5 節とも「なし」

## 数

- dupscan を打った回数: タスクで 6 (capabilities 1・sweep 2・index 1・callers 1・gate 1)。再現と修正の確かめで 20 前後 (sweep・gate・gate -v。手元で build した版の sweep を含む。正確な数は未計測)
- grep に戻した回数 2 (下の F6)。文字列・既存の印・挿入位置の定義 1 件の狙い撃ちは数えない
- 役に立った場面 4: sweep が 3 組を 1 回で出した / gate -v が人数の食い違いを「人数 2・同じ本体の単位 … L1m 3」と正確に言った /
  `index -name Stale` (1,325 B) で、効いていない除外を 1 行に組み立てる関数が sweep 側に無いと分かった / `callers -limit 0 GroupMiss` (697 B) で表示している所が report.go の 1 か所だけと分かった

## 所見

### F1 (黙って効かない・修正済み): allow-group の人数を間違えると、sweep は組を黙って出し直す

- 場面: 印が効くかを確かめるため、`foreground/exec.go` の `allow-group 3` をわざと `2` にして `dupscan sweep src/tuikit`
- 期待: 「印はあるが人数が合わない」と出る
- 実際: 節 2 に Exec の組がそのまま戻るだけ (印には触れない)。理由は `dupscan gate -v` の「効いていない除外」の下にだけ
  「dupscan:allow-group の人数と、同じ本体の単位の数が合わないので効かせていない (… 人数 2・同じ本体の単位 L1 1・L2 1・L3 1・L1m 3)」と出た (gate -v は 11,210 B)
- 対応: sweep の冒頭に `🚨 効いていない allow-group (組は出し続ける。寄せるか、確かめてから人数を直す): <印の場所> (<段ごとの数>)` を出す

### F2 (使い方が分かりにくい・修正済み): 印を足した後の「なし」が、印で消えたのかを言わない

- 場面: 3 組に印を付けて sweep し直した
- 実際: 5 節とも「なし」。元から組が無いのと区別できず、F1 の再現をするまで印が効いているかを確かめられなかった
- 対応: 冒頭に `印・除外で静かにした組 (節 1・2): N` を出す (tuikit で 3、repo 全体で 10。最初の版は節 1 と節 2 で同じ組を 2 回数えて 14 と出していた。下の反証レビュー)

### F3 (文書のずれ・修正済み。965 の F3 と同じ箇所): 冒頭の案内が「自分の側に書けば消える」と読める

- 「単位の直前に dupscan:allow を書くと静かになる」を「組の全員の単位の直前に … (1 人だけに書いて組ごと静かにするなら … allow-group)」に直した

### F4 (修正の途中で自分が入れた誤報・修正済み): 起点を絞ると、module をまたぐ組の印を食い違いと読む

- F1 の最初の版を `dupscan sweep src/treefiler` に当てると 🚨 が 4 本出た (`filer/helpers_test.go:75`・`:84`、`filer/waitkey_pty_test.go:62`・`:69`。どれも人数 2〜4・同じ本体の単位はどの段も 1)。
  repo の root から打つと 0 本
- 原因: allow-group の人数の照合 (`internal/exclude/exclude.go` の `groupCommentRule`) は、起点の中の単位だけで同じ本体を数える。起点の外のメンバーは数えられない
- 対応: 起点が repo の一部なら、起点の中の **L1 の**同じ本体の単位が人数より多い印だけを 🚨 で出し、ほかは「人数の合わない allow-group N 個は、起点の中の L1 の同じ本体の単位が人数より少ないので出していない (…)」と数だけ言う。
  起点の外を足しても数は減らず、L2・L3・L1m は L1 の一致を含むので、L1 で多ければどの段でも人数に合わないと決まる。粗い段だけで多いのは、起点の外の相手と L1 でちょうど合う形と区別できない
- 未確認: gate を起点を絞って打ったとき、同じ印が gate -v で「人数の食い違い」に出るか (sweep と同じ数え方なら出る。gate は cwd の配下だけを走査する。954 の P4)

### F5 (雑音・見送り): gate の要約の「効いていない除外 N」に、sweep のためだけの印が入る

- tuikit の `ScrollGlide.Offset` の印 (節 15 の組。gate の下限 20 節では組にならない) が、正しい印のまま「効いていない除外」に数えられ、要約が 9 になった。
  `gate -v` の理由には「sweep のために置いたのなら残してよい」とあるので、要約だけを読むと直すべき印があるように見える
- 見送り: 要約から外すか別に数えるかは設計の判断 (要約の数を頼る人・検査があるかを確かめてから)。ここでは記録だけ

### F6 (入口): 新しい helper を書いてから dupscan で引いた・型のメソッドの一覧を grep で取った

- `sweepExclusionText` を書いた後に、PreToolUse の hook の案内で `index -name Stale` を打った (書く前に引くべきだった。結果は既存なし)
- `exclude.Set` のメソッドの一覧を `grep -n '^func (s \*Set)' internal/exclude/*.go` で取った。`dupscan index -file` で取れることを思い出さなかった
- `type ScrollGlide` の構造体の中身を `grep -A12` で読んだ (Go の def は無い)
- 提案: 無し (入口の測定は 948 の範囲)

### F7 (使い方・見送り): 関数に付けた allow-group は、その中の同じ形のクロージャの組を消さない

- テストの fixture (`goTwoSinks`) で、`captureOut` に `allow-group 2` を付けると関数の組 (L1m) は消えるが、中のクロージャどうしの組 (L1 ×2 節 17) は残った。
  単位の `dupscan:allow` は囲む関数が許可済みならクロージャにも効く (`dropAllowedGroups`) ので、印の種類で扱いが違う
- 見送り: 組の印をクロージャへ広げるかは意味の判断 (中のクロージャだけが別の写しになった場合を覆うことになる)。記録だけ

## treefiler (ユーザーの追加依頼)

別のセッションが同じ日に dogfood を済ませていた (965、commit「test(treefiler): テストの helper を testing.TB で受け、手で書いていた同じ手順を寄せる」)。重ねて寄せ直しはせず、その後の木で修正後の sweep を当てて残りを見た:
節 1・5 は「なし」、節 2 の 7 組・節 3 の 3 対・節 4 の `take` ×5 は、どれも 965 の「見送り」に挙がったもの (settings の表のクロージャ・mutex の getter・型ごとの take)。新しく寄せるものは無かった。

## 修正

- commit「feat(dupscan): sweep の冒頭に印で消した組の数と、人数の合わない allow-group を出す (issue 966)」: F1・F2・F3
- commit「fix(dupscan): sweep の起点を絞ったとき、範囲の外と区別できない allow-group の食い違いを 🚨 にしない (issue 966)」: F4 と、1 周目の反証レビューの P1 2 件
- commit「fix(dupscan): sweep の「静かにした組」を、除外で消えた組の部分集合が節 2 に残っても負にしない (issue 966)」: 2 周目の反証レビューの P1
- 確かめた方法: 変異 10 本 (mutate-verify、どれも狙った assert で赤): 消した数を 0 に固定 / 人数違いの行を出さない / 節 2 の分を数えない /
  起点を見ずに全部出す / `groupOver` を立てない / 起点が repo 全体でも出さない / `groupOver` を粗い段でも立てる / 数え方を節ごとの差の和に戻す /
  数え方を「印が無ければ出た組の数との差」(1 周目の直し) に戻す / 同 (最初の版)。
  実物 (最終版): root 10・tuikit 3 (Exec の印を 2 に崩すと、起点 tuikit で「出していない 1 個」、root で 🚨 1 本)・treefiler 0 と「出していない 4 個」
- 検査: `make -C src/dupscan test lint`・`make test-lint`・`dupscan gate` が rc=0 (他のセッションの 965 F2・970・節 6 を取り込んだ rebase の後も)。
  ルートの `make test` は `tests/claude/test_dangling_symlinks.sh` だけが赤: `~/.claude/agents/` に、別の commit「refactor(agents): 使われていない global の agent 5 本を消し、…」で消えた agent 5 本の link が残っている。この変更と無関係で、`./setup.sh` で片付く (ユーザーの環境の操作なので手を出していない)

## 反証レビュー (敵対的、opus、読み取りのみ、2 周)

修正の設計と「効いた」の主張を含むので、issue の反証レビューの代わりに修正の差分へ敵対的レビューを 2 周通した。issue の本文そのものの反証レビューは省いた (所見の記録で、主張は上の検証で裏を取った)。

- 1 周目: P1 2 件を採用・P2 1 件と P3 2 件を記録
  - P1 (採用): 静かにした組を節ごとの差の和で数え、節 1 から消した 20 節以上の組を節 2 でも数えた。テストで 2 と出るのを再現して直した
  - P1 (採用): `groupOver` を粗い段でも立て、起点を絞ると正しい印を 🚨 で出した。L1 で 1・L2 で 3 の fixture で再現して直した
  - P2 (記録・未対応): 起点を絞ると、L1 で人数に合わない allow-group が粗い段 (L2・L3・L1m) でちょうど人数の別の組に効き、その組を黙って消す。
    `groupCommentRule` の段を順に見る loop が起点の中の単位にだけ走るため。この変更より前からある。gate・report を起点を絞って打った場合も同じはず (未確認)。
    再開の trigger: 起点を絞った sweep / gate で、印を書いていない組が消えている報告が出たとき
  - P3 (記録): 起点を絞ると、全員が起点の中にいる本当の人数の書き違い (少なく書いた・写しを消した) も「数だけ」に落ちる (区別できないので意図どおり。README に書いた)。
    同じ組の 2 人に人数違いの印を書くと 🚨 が 2 行出る (害は無い)
- 2 周目: P1 1 件を採用・P2 1 件と P3 2 件を記録
  - P1 (採用): 1 周目の直しの数え方は、節 1 で除外した組の部分集合が節 2 に残ると引きすぎ、負になる。パスの除外は levels を省くと L1m に効かないので起こる。
    `-levels L1,L2,L3` + `gen/**` のパスの除外で -1 と出るのをテストで再現して直した
  - P2 (記録・未対応): levels を省いたパスの除外は L1m の組を消さない (`exclude.go` の `check()` は `r.levels` に LM を入れない)。生成コードの dir を除外しても、メンバー名だけ違う組が sweep の節 2 に出続ける。
    `check()` のコメントは「Levels が無ければ全部」と読めるので、意図かは未確認。再開の trigger: パスの除外の中の組が節 2 に出るという報告
  - P3 (採用): 数だけ言う行の「人数以下」→「人数より少ない」(L1 でちょうど人数なら印が効くので、この行に来るのは少ないときだけ)
  - P3 (記録): 🚨 の行の段ごとの数は起点の中の数で、そう書いていない
  - 観点 2 (L1 で多ければどの段でも合わない)・観点 3 (追加したテストの素通り) は壊せなかった

## 進捗

- [x] tuikit の 3 組の判定と印
- [x] F1・F2・F3 の修正と push
- [x] F4 の修正と、敵対的レビュー 2 周の P1 3 件の修正・push
- 見送り: F5・F6・F7 (理由は各節)。記録: 反証レビューの P2 2 件 (再開の trigger は上の節)
