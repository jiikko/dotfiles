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

- dupscan を打った回数: タスクで 6 (capabilities 1・sweep 2・index 1・callers 1・gate 1)。再現と修正の確かめで 10 (sweep 6・gate 3・gate -v を含む)
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
- 対応: 冒頭に `印・除外で静かにした組 (節 1・2): N` を出す (tuikit で 3、`src` 全体で 14)

### F3 (文書のずれ・修正済み。965 の F3 と同じ箇所): 冒頭の案内が「自分の側に書けば消える」と読める

- 「単位の直前に dupscan:allow を書くと静かになる」を「組の全員の単位の直前に … (1 人だけに書いて組ごと静かにするなら … allow-group)」に直した

### F4 (修正の途中で自分が入れた誤報・修正済み): 起点を絞ると、module をまたぐ組の印を食い違いと読む

- F1 の最初の版を `dupscan sweep src/treefiler` に当てると 🚨 が 4 本出た (`filer/helpers_test.go:75`・`:84`、`filer/waitkey_pty_test.go:62`・`:69`。どれも人数 2〜4・同じ本体の単位はどの段も 1)。
  repo の root から打つと 0 本
- 原因: allow-group の人数の照合 (`internal/exclude/exclude.go` の `groupCommentRule`) は、起点の中の単位だけで同じ本体を数える。起点の外のメンバーは数えられない
- 対応: 起点が repo の一部なら、起点の中の同じ本体の単位が人数より多い印だけを 🚨 で出し、少ない印は「… 個は出していない (起点の外のメンバーと区別できない。repo の root から打つか、gate -v で見る)」と数だけ言う
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
- F4 の直しは下の進捗の commit
- 確かめた方法: 変異 6 本 (mutate-verify、どれも狙った assert で赤): 消した数を 0 に固定 / 人数違いの行を出さない / 節 2 の分を数えない /
  起点を見ずに全部出す / `groupOver` を立てない / 起点が repo 全体でも出さない。実物は tuikit (正しい印で 3、崩すと 2 と 🚨 1 本)・treefiler (🚨 0・数だけ 4)・root (🚨 0・14)

## 反証レビュー

所見は観察の記録だが、修正の設計と「効いた」の主張を含むので通す (結果は進捗に書く)。

## 進捗

- [x] tuikit の 3 組の判定と印
- [x] F1・F2・F3 の修正と push
- [ ] F4 の修正 (敵対的レビュー待ち)
- 見送り: F5・F6・F7 (理由は各節)
