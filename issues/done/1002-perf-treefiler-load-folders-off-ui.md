# 1002 (perf): treefiler がフォルダに初めて入るときの読み込み・並べ替えを UI の外へ出す

起票日: 2026-10-30

## 概要

フォルダに初めて入る (`l` / Enter / Space / Tab) と、中身の読み込み・ノードの生成・名前の自然順の並べ替えを UI の処理の中で行い、
2 万件のフォルダで 48〜68ms 止まる (issue 995 の tfperf と使い捨てのテストで実測)。並べ替えの比較を軽くし、読み込みから並べ替えまでを裏へ移す。

## 詳細 (実測)

使い捨てのテストで、初めて入るときの内訳 (2026-10-30。直す前):

| | 5,000 件 | 20,000 件 |
|---|---|---|
| `l` のキーの処理全体 | 10.8ms | 48.2ms |
| 中身の読み込み (readEntries) | 4.8ms | 20.0ms |
| 名前の自然順の並べ替え (sortKids) | 3.8ms | 22.7ms |
| ノードの生成 (newNodeAttr) | 1.1ms | 4.5ms |

tfperf (実物のバイナリ) では 20,000 件で キーの処理 68ms・キーから最初の書き込みまで 86ms。j/k の移動はフォルダを読まない (setCur はカーソルの付け替えだけ)。

## 対応方針

### 手順 1: 名前順の比較で小文字の名前を作り直さない

比較 (cmpName) が比較のたびに 2 つの名前を strings.ToLower していた。ノードを作るときに `lower` を 1 回だけ作る。

### 手順 2: 打鍵で初めて開くフォルダの読み込みを裏へ移す

- 裏へ移すのは打鍵の 2 経路だけ: `l` / Enter (descend) と Space / Tab (開閉)。起動時の root・`a` で作る・`>`・`:`・前回の場所の復元・
  ライブ更新の読み直しは同期のまま (どれも結果をその場で使う。`>` は既に裏の仕事)
- 頼むと、裏の goroutine が readEntries → newNodeAttr → sortKids (頼んだ時点の並べ方を写して使う) まで行う。UI はノードに
  `loading` を立て、**loadWait (12ms) だけ結果を待つ**。間に合えば今と同じくその場で取り込む (5,000 件 ≒ 7ms までは見た目が変わらない)。
  間に合わなければ、その打鍵の残り (潜る / 開く) を「続き」として覚えて戻る
- 取り込みは Advance (pane.go と同じ形)。読み込み中は Animating を立てる (16ms の tick で取り込む。Busy の 80ms では最大 80ms 遅れる)。
  最下行の右に `⠋ reading <名前>` を出す (`>` の exploding と同じ場所。木の配置には仮の行を足さない)
- 取り込むときの判定:
  - ノードが既に読み込み済み (その間に同期の読み込みが走った) → 結果を捨てる
  - 並べ方が頼んだ時点と違う → 今の並べ方で並べ直す
  - 読めなかった → `読めません: <名前>` (今と同じ知らせ)
  - 続きは、カーソルがまだそのフォルダの上にあり、層 (板・入力欄・確認) とタイルが開いていないときだけ行う (潜る: 見える子があれば開いて
    最後にいた子か先頭へ / 開く: 開く)。カーソルが離れていたら、読み込んだだけにする (勝手にカーソルを動かさない)
  - **次のキーが来たら、そのキーより先に読み込みを待って続きを済ませる** (flushLoad。ctrl+c は待たない)。先打ちの `l j`・`l l`・
    `Space Space` は、同期で読んでいた頃と同じ順と意味になる。止まらないのは、何も打たずに待っている間だけ。待つのは頼んでからの合計で
    1 秒 (loadFlushWait) までで、一度諦めた読み込みは二度と待たない (届いていれば待たずに取り込む)

### 状態 × 読み手 (期待値)

| 状態 \ 読み手 | l / Enter | Space / Tab | 同期の読み込み (a・>・:・復元) | ライブ更新の reload | 並べ方の変更 (S・設定) | カーソルが離れる | Close |
|---|---|---|---|---|---|---|---|
| 未読込 | 頼む (12ms 待つ) | 頼む (12ms 待つ) | 今までどおり同期で読む | 対象外 (loaded だけ) | 対象外 | — | — |
| 読み込み中 | 次のキーの前に済ませる (続きの後に効く。諦めた後なら頼み直さず続きを覚え直す) | 同左 | 同期で読む。後から届いた結果は捨てる | 対象外 (loaded でない) | 取り込むときに並べ直す | キーで離れる: 先に続きを済ませてから離れる / キーを通らずに離れる (ライブ更新等): 続きは発火しない | 続きだけを消す。結果は開き直した後の Advance で取り込む (glogx の F は同じ Model を開き直す) |
| 読み込み済み | 今と同じ | 今と同じ | 今と同じ | 今と同じ | 今と同じ | 今と同じ | 今と同じ |
| 読めなかった | 今と同じ (readErr。空として扱う) | 同左 | 同左 | 同左 | 同左 | 同左 | 同左 |

- 経路の光 (pulse.go) と右の欄 (pane.go) は、続きで Advance の中でカーソルが動いたことも「カーソルの変化」と数える (打鍵で潜った結果なので、
  今の l と同じ扱いでよい。dotfiles-02 の指摘)

## 設計の反証レビュー (opus) で直した点

P1 2 件・P2 4 件・P3 7 件。取り込んだ:
- glogx の F は Close の後も同じ Model を開き直す → Close は続きだけを消す。読み込みの記録は開き直した後の Advance で取り込んで下ろす
- 12ms の壁時計でテストが非決定になる → `LoadWait` を差し込み口にし、テスト (filer・treefiler・glogx) と tfprobe は負 (同期)。非同期は止められる読み手 (`Model.loadRead`) を入れた load_test.go だけで見る
- 続きの発火が緩い (j → k で戻ると潜る・c で畳んだ直後に開き直す・板の下で動く) → 次のキーで捨てる (HandleInput の入口)。発火するときも層とタイルが開いていないこと
- 止まったマウントで tick が回り続け goroutine が増える → 速い tick は頼んでから 1 秒 (`loadHot`)、その後は Busy の遅い周期。裏で読むのは 2 本 (`loadMax`) までで、超えたら同期
- `m.moving` は次の Advance で上書きされる → Animating と Advance の戻り値に「読み込み待ち (1 秒以内)」を足す。Busy にも足す
- goroutine の中でパッケージ変数 `readBulk` を読むと、テストの差し替えと競合する → 読み手は頼んだ時点で UI 側で捕まえて渡す (実装中に、テストが `readBulk` を差し替える形そのものが裏の走査と競合すると分かり、差し込み口を Model の欄にした)
- P3: 読み込み中の Space は「開く」に固定 / ensureLoaded は同期のまま残し非同期の口は別 (requestLoad) / 取り込みは setKids を通して kidsVer を増やす / 最下行は最後に頼んだフォルダの名前を出す / tfprobe は同期
- 記録 (直さない): 止まったマウントでは、読み込みの記録が返らない読み込みのノードを掴み続ける (2 本が上限)

## 進捗

- [x] 手順 1: ノードに `lower` を持ち、比較で小文字を作り直さない。並べ替え 2 万件 22.7ms → 9.3ms・5,000 件 3.8ms → 1.8ms (使い捨てのテスト)。
  テスト `TestNameOrderIgnoresCase` (newNodeAttr を通す)。変異: newNodeAttr で lower を入れない → red
- [x] 手順 2: load.go。テスト load_test.go (-race で 3 回緑)。最初の版の変異で red を確認 (bin/mutate-verify。続きを消す形は後で作り替えた): 入口で続きを消さない →
  TestOtherKeyDropsLoadContinuation (今は TestNextKeyRunsAfterLoad) / 取り込みで読み込み済みを見ない → TestSyncLoadWinsOverLateResult / 並べ直さない →
  TestLateResultUsesCurrentOrder / Close で続きを消さない → TestCloseDropsContinuationButTakesResult / loadMax を見ない → TestLoadsAreBounded /
  loadHot を見ない → TestLoadStopsHotAfterLoadHot / Animating に足さない → TestDescendLoadsOffUI / 読み込み中を見ない → TestRepeatedKeyKeepsOneLoad
- [x] make -C src/treefiler lint・test、仮想化の等価性 (TREEFILER_VIRT_SEEDS=300) 69 秒、make -C src/glogx test: すべて rc=0
- [x] 実物での比較 (tfperf。5,000 件と 20,000 件のフォルダ、`l h j l h`・1500ms、直す前と後を交互に 2 回ずつ):

  | | 直す前 run1 / run2 | 直した後 run1 / run2 |
  |---|---|---|
  | 20,000 件に入る `l` の handle_ms | 78.2 / 57.1 | 14.2 / 13.3 |
  | 同 draw_ms (キーから最初の書き込み) | 98.0 / 65.7 | 31.7 / 30.6 |
  | 5,000 件に入る `l` の handle_ms | 24.0 / 18.7 | 13.2 / 16.4 |

- [x] 実装の敵対的レビュー (opus、4 周)
  - 1 周目 P2 2 件・P3 6 件。直した: root を上げた後に旧 root の `lower` が絶対パスのまま残り、並べ方を変えると先頭へ飛ぶ (raw と lower を
    setRaw で一緒に書く。reroot・goto の取り消し・タイル)・先打ちのキーが潜る前の状態に効く (`l j` が A の隣へ・`l l` が 1 段) → 次のキーの前に
    続きを済ませる形へ作り替えた・カーソルの守りと木の外の失敗の知らせ。記録: glogx の `U` の板は filer にキーを渡さず続きが残る (潜るのは打鍵の意図どおり)・
    細い幅で reading がステータスバーごと落ちる・goroutine の panic は端末を戻さない (walk / watch も同じ)
  - 2 周目 P2 1 件・P3 4 件。直した: 止まったマウントで l を押し続けるとキーごとに 1 秒止まる (頼んでからの合計で切り、諦めたら二度と待たない)・
    ctrl+c も待ちの後 (先に終わる)。記録: 次のキーが何に効くかが壁時計で決まる (確認の板に名前が出る)・glogx で l の直後に F で閉じると潜らずに閉じる
  - 3 周目 P2 1 件・P3 2 件。直した: 諦めた後・上限の後に届いていた結果を覗かずに続きを捨てる (待たずに 1 回覗く)・合計で切ることを固定するテスト。
    記録: q と esc は待つ (タイルや層を閉じるのにも使い、待たないと閉じた後に続きで潜る。コメントを正確に直した)
  - 4 周目 P1・P2 なし。P3 1 件 (上限切れの枝の覗きを守るテストが無い) を直した
  - 変異で red を確認 (上の分に加えて): setRaw をやめ raw だけ書く → TestRerootKeepsNameOrderAfterResort / 次のキーで続きを捨てる → TestNextKeyRunsAfterLoad /
    諦めた後も続きを残す → TestNextKeyGivesUpOnStuckLoad / カーソルの守りを外す → TestContinuationNeedsCursorOnFolder / inTree を見ない →
    TestLoadFailureNoticeOnlyInTree / gaveUp を見ない → TestStuckLoadIsWaitedOnce / ctrl+c の前に待つ → TestQuitDoesNotWaitForLoad /
    キーごとに 1 秒 → TestFlushWaitCountsFromRequest / 覗かない → TestFlushTakesArrivedResultAfterGivingUp / 上限切れの枝で覗かない → TestFlushTakesArrivedResultAfterLimit

- [x] レビューを直した最終の版での確認 (2026-10-30): root の make test rc=0。tfperf (直す前と最終を 1 回ずつ) で 20,000 件に入る `l` の
  handle_ms 57.0 → 12.4・draw_ms 79.1 → 13.5

## 残タスク (スコープ外)

- 大きいフォルダから `h` で戻る打鍵は前後とも 9〜12ms (読み込みではなく配置の組み直し)。未調査
- ライブ更新の読み直し (reload) は同期のまま (issue 693 で 5 万件の変化で約 184ms と記録済み)
