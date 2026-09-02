# 159 retro: snapshot ラッパーの自己修復・concat の破損検出・glogx 読み直しの非同期化 2026-09-02

起票日: 2026-09-02

## このセッションでやったこと

1. issue 152: Claude Code の shell snapshot で壊れるラッパー (動画系 6 関数 / t・tt) を self-heal に (`359b12e` / `c40b8b2`)
2. issue 143: concat に音声 time_base チェック・境界デコード検証・ffprobe 失敗の fail-closed を入れた (`1584bb3`)
3. issue 146: glogx の git log 自動追従の読み直しを Cmd に出し、Update を止めなくした (`43b34a6`)
4. issue 148 段階 1: `src/svcdoctor` (壊れた launchd 登録の検出、読み取り専用) を新設
5. issue 148 段階 2: `src/diskdoctor` (掃除候補の走査 + dry-run 一覧、削除なし) を新設

## 反省・気づき (各項目に切り出し先を提案。実行はユーザーの判断待ち)

### 1. 「Update の中で fork しない」を最初に書いた配線テストは検知できなかった

`TestGitLogWatchWiredThroughUpdate` に「Update 直後は commits が入れ替わっていない」を足したが、
「Cmd を作る時点で同期に git を読み、結果を握った Cmd を返す」変異は**モデルを触らないので通った**。
検知できたのは「Cmd を作った後に PATH を空にして、Cmd の実行が初めて失敗する」形
(`TestGitLogReflectRunsGitInsideCmd`)。副作用の**タイミング**を固定したいときは、状態の観測では
足りず、「その時点で副作用が起きたら失敗する環境」を挟む必要がある。

- 変異 4 本のうち 1 本が green で残り、テストを作り替えて red にした ([`mutation-verify-new-tests.md`]
  の通常運用で拾えた)。ルール改訂は不要
- **切り出し先案**: `mutation-verify-new-tests.md` の「よくある『守っていないテスト』の形」に 1 項
  (「副作用の**時点**を主張するテストは、状態の観測では固定できない。その時点で副作用を失敗させる
  環境 (PATH 空・fake の reject) を挟む」)。却下でもよい (既存の「検証したいコードが本当に
  実行されているか」で読める)

### 2. mock の文字列一致がテスト同士で干渉した (`tbase_002` ⊂ `atbase_002`)

新しい fixture 名 `atbase_002` が既存の `grep -q "tbase_002"` に部分一致し、映像 time_base 不一致が
先に発火して音声側のテストが落ちた。fixture 名で分岐する mock は**部分一致**なので、新しい名前を
足すときは既存パターンの substring になっていないかを見る必要がある。

- **切り出し先案**: `tests/zshrc/concat/test_helper.sh` の mock 冒頭コメントに 1 行
  (「分岐は部分一致。新しい fixture 名は既存パターンを含まない名前にする」)。ルール化は不要

### 3. 「検査できなかったを緑にしない」は、足そうとした 1 箇所より広かった

issue 143 は音声 time_base の helper に第 3 状態を要求していたが、実際に落ちたのは**既存の
`__concat_get_audio_info`** (ffprobe 失敗 → 空 → 「再エンコードが必要」の誤案内、先頭ファイルで
失敗すると音声チェックごと skip)。新設だけ直しても、同じ形の既存 helper が隣にある限り
fail-closed にならない。同型の grep (`2>/dev/null | head -n1`) で video / audio の 2 箇所を同時に直した。

- `__concat_get_video_frame_rate` / `__concat_get_video_time_base` / `__concat_get_duration` には
  同じ形が**残っている**。frame_rate は表示専用、time_base は空なら `target_timescale` の計算から
  外れて不一致扱いにならない (= 素通り)、duration は空を検証側が失敗扱いにしている。
  video time_base の空は素通りなので同じ穴だった → **同セッションで修正済み** (`__concat_get_video_time_base`
  も rc を返し、呼び出し側で拒否。`test_concat_time_base.sh` Test 6。変異: `|| return 1` を外す → red)。
  frame_rate は表示専用、duration は検証側が空を失敗扱いにしているので残してよい

### 5. src/README.md の「GO_PROJECT_DIRS に登録」が自動発見 (issue 080) 以後も残っていた

新プロジェクトを足す手順書が古い手順を指していた。読んでから Makefile を見たので気づけたが、
手順書だけ信じると存在しない登録先を探す。同じ commit で直した。**切り出し先**: なし (直した)

### 6. 「構造上ありえない値」を守るガードは、変異で区別できない (diskdoctor の合計)

「blocked / failed を合計に足さない」ガードは、blocked / failed の Result が構造上 Size 0 なので
外しても既存テストが green だった (M7)。ガードは将来の変更に対する防御で、今の入力では区別できない。
合計の計算を `sumDeletable` に切り出し、Size を持つ failed を直接与えるテストにして red を見た。
教訓: **ガードが守る入力が今の生成経路から出ないなら、生成経路を経由せず値を直接与えるテスト**にする。
**切り出し先案**: `mutation-verify-new-tests.md` の「守っていないテストの形」に 1 項。却下でもよい
(「差が出ない状況しか作っていないか」で読める)

### 7. ディスク走査は実機で 1 回流すだけで設計の穴が 3 つ出た

`/opt/homebrew/etc` の雑音 / TCC で読めないコンテナで entry 全体が失敗 / `~/.goenv` が深さガードに
弾かれる、の 3 つは fake の HOME では作れず、実機で流して初めて出た。読み取り専用のツールなので
早く実機で流せた。④ の削除は実機で流せないので、この段階で穴を出し切っておく価値がある。
**切り出し先**: なし (記録)

### 4. 実 CLI の契約を先に測ってから mock を書いた (良かった点)

ffprobe の列順 (`codec_name,sample_rate,channels,time_base`) と「音声なし → 空・rc 0 / ファイルなし →
rc 1」を stdout / exit code を分けて実測し、mock をそれに合わせた。issue の再現 1 / 2 を実 ffmpeg で
流して拒否を確認できたのはこのおかげ。**切り出し先**: なし (既存ルールどおり)

## 残課題

- [x] 項目 1 の切り出し → **項目 6 と統合して 1 項**を `mutation-verify-new-tests.md` に足した
- [x] 項目 2 の mock コメント追記 → `tests/zshrc/concat/test_helper.sh` の ffprobe モック冒頭
- [x] 項目 6 の切り出し → 上記に統合

### 切り出しの判断 (2026-09-02)

項目 1 と 6 は**骨格が同じ**だった: どちらも「今の生成経路 / 状態のスナップショットからは、
変異の有無で同じ値しか出ない」形で、違うのは処方だけ (時点を主張するなら副作用が失敗する環境を
挟む / ガードなら生成経路を経由せず値を直接与える)。**別々に 2 項足すと、毎セッション全文読まれる
ルールが同じことを 2 回言う**ので、「その観測は、変異の有無で違う値を出すか」という 1 項にまとめ、
2 つの処方を子項目にした。

既存項目「差が出ない状況しか作っていないか (要素 1 個、空集合、恒等条件)」の一般形として位置づけた
(retro 起票時は「既存項目で読める」として却下候補にしていたが、**既存項目は「そういう状況を作るな」
までで、どうすれば区別できる観測になるかの処方が無い**。処方の方が実務では効くので採用)。
