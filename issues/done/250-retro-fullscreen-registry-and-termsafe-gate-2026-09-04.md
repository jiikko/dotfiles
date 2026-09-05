# retro: 227 (全画面ビューアの出典一本化) / 228 (doctor の live 経路を termsafe へ) 2026-09-04

セッション: dotfiles-c2 (worktree `~/wt-227228`)

## 何をやったか

- **227**: `activeFullScreen` (`src/glogx/fullscreen.go`) を単一の出典にし、見送り / 描画 /
  hint / 復元の破棄 / キー routing の 5 サイトを導出。配線漏れは `exhaustive` lint と
  ID ごとの表テストの 2 段で止まる。`U` の非対称も解消 (doctor で効くように / ダッシュボードは
  意図的に受けない旨を明記)
- **228**: `termsafe` を `src/termsafe` の独立 module へ出し、doctor の live 経路と CLI の
  両方が同じ関門を通る形にした。同一性を持つ値 (パス / plist のラベル) は書き換えず落とす

## 気づき (切り出し先の提案つき。切り出しの実行はユーザーの判断待ち)

1. 🚨 **変異検証を未コミットのまま `git checkout -- <file>` で回し、自分の修正を消した**。
   敵対レビューの指摘 7 件を直した直後、その修正が未コミットの状態で変異スクリプトを走らせ、
   復元のたびに**変異ではなく修正の方**が捨てられた。7 ファイル分を書き直した。
   `mutation-verify-new-tests.md` が名指しで禁じている形で、ルールは読んでいたのに踏んだ。
   → 切り出し: **`_claude/rules/mutation-verify-new-tests.md` の「復元の作法」へ 1 行**。
   既存の記述は「`git checkout -- <path>` を常用手順にしない」だが、**発動点が
   「レビュー指摘を直した直後」**であることを書き足すと踏みにくい (直後は必ず未コミット)

2. **変異の判定を「テスト名」で鍵にして、2 package の同名テストが衝突した**
   (`doctor/disk` と `doctor/svc` の `TestFormatSanitizesUntrustedText`)。片方が red でも
   もう片方の PASS で上書きされ、「この変異は検知されない = 無検査」と一度誤読した。
   → 切り出し: `mutation-verify-new-tests.md` の「ケース名ごとの pass/fail で判定する」に
   **package 名も鍵に含める**を 1 行足す

3. **「入口 1 箇所で無害化」を素直にやると、削除の照合と提示コマンドを壊す**。表示のために
   パスやラベルを書き換えると、画面に出ている名前と実際に触る対象が食い違う。
   → 切り出し: `_claude/rules/survey-receiver-guards-before-passing-new-values.md` へ
   実例として追記 (「表示のための正規化が、同じ値を identity に使っている受け側を壊す」形)

4. **敵対レビューが見つけた最大の穴は「テストの fixture が分岐を殺していた」**
   (brew の警告と `Unavailable` を同時に立てると、`brewSection` の早期 return で警告の row が
   0 個になり、警告側の無害化を外しても緑)。
   → 切り出し: **却下**。`mutation-verify-new-tests.md` の「fixture は退行したら見えるように
   なる場所に置く」が既に同じことを言っており、足すなら実例の追加だけ

5. **サンドボックスの書き込み制限で `go test` が "file too large" で落ち、テスト失敗に見えた**
   (実体は `/var/folders/.../go-build*/testlog.txt` への書き込み。全テストは PASS していた)。
   `GOTMPDIR` を scratchpad へ向けると通る。別セッションも同じ症状を疑っていたので共有した。
   → 切り出し: 弱い。ルール化するほどではないが、10 分溶かした

6. **並行セッション 4 つが、共有ツリーの未 push commit を「私のもの」と誤認した**。3 セッションから
   同じ問い合わせが来て、そのたびに「私の claim は push 済みの別 commit」と訂正した。
   帰属に使えるのは commit が触ったファイルと本人への確認だけ、という規律は正しく効いた。
   → 切り出し: **却下** (`commit-with-pathspec.md` に既に書いてある。今日はそれが機能した側)

7. **敵対レビューを 2 本とも「壊せ」で投げたら、2 本とも本物を出した**。228 側は復元経路と
   同一性 (P1 2 件)、227 側は「強制手段が ID を足した後にしか発火しない」(P1 1 件)。
   どちらも**自分では思いつかなかった軸**で、特に 227 の指摘は「機構は主張どおり動くが、
   一番安い経路 (switch の前に if を 1 本) を通ると発火しない」という形だった。
   → 切り出し: **却下** (`adversarial-review-own-safeguards.md` が既に要求している工程で、
   今日はそれが効いた側。ただし節 8 の「脅威モデルと検出しない形を先に書く」を**作る前**に
   やっていれば、AST ゲートは 1 周目で入っていた)

8. **識別を見ないテストは「一覧が出ていない」までしか守らない**。227 の描画テストは
   `viewLines` の doctor ↔ status を入れ替える変異を素通りした (一覧の SHA が無いことしか
   見ていなかった)。同型は 228 側でも出た (brew の fixture が分岐を殺していた)。
   → 切り出し: **却下** (`mutation-verify-new-tests.md` の「属性の存在だけを見ていないか」
   「fixture は退行したら見えるようになる場所に置く」が既に同じことを言っている)

## 決着 (2026-09-05)

- 1 → `mutation-verify-new-tests.md`「復元の作法」に「発動点はレビュー指摘を直した直後。変異の前に commit する」を追記
- 2 → 同ルールの「ケース名ごとの pass/fail 一覧」に「鍵は package 名 + ケース名」を追記
- 3 → `survey-receiver-guards-before-passing-new-values.md` に「表示のための正規化が identity を壊す」を追記
- 4 / 6 / 7 / 8 は本文どおり却下。5 は弱いので切り出さない (ここに記録するだけ)

## 残課題

(なし)
