---
name: avfoundation-reference
version: 1.0.0
description: AVFoundation (AVPlayer / AVPlayerLayer / AVPlayerItemVideoOutput / AVAsset) の落とし穴・文書化されていない実装挙動・debugging チェックリストを集約した reference skill。Swift / Objective-C で AVPlayer を使った動画再生 / seek / scrub / frame stepping を実装・debug するときに発火。VLCKit ではなく **AVFoundation 系** の問題に特化。
---

# AVFoundation Reference & Debugging Skill

AVPlayer / AVPlayerLayer / AVPlayerItemVideoOutput / AVAsset まわりの **公式仕様だけでは
読み取れない実装挙動** と **debug 手順** を集約する skill。Apple Developer Documentation
を引いても載っていない「やってみないと分からない」落とし穴が中心。

## 発火条件

以下のいずれかに該当したら本 skill を必ず一度通す:

- AVPlayer の seek / scrub / frame stepping を実装する
- AVPlayer の playback rate / pause / play 挙動を変える
- AVPlayerLayer の表示 frame と playback clock の関係を扱う
- AVPlayerItemVideoOutput の attach / detach lifecycle を変える
- AVPlayer の completion handler / KVO observer の thread モデルを扱う
- 動画の「コマ送り」「scrub」「click seek」「frame-stepping」体感が想定と異なる

## 鉄則

### 1. 観測 first、推測 second

**ユーザーが見ている現象** (= 映像 / UI / 音声) を **直接観測** する手段を、修正コードを書く
前に必ず通す。log の数値 (= playback clock / cached currentTime) だけでは「ユーザーが見て
いる frame」と乖離していることが頻繁にある。

具体的:

- **Instruments の Core Animation ツール** で `AVPlayerLayer` の表示 frame 更新タイミングを観測
- `AVPlayerItemVideoOutput.copyPixelBuffer(forItemTime:itemTimeForDisplay:)` の `itemTimeForDisplay` を log に出して、AVPlayer の playback clock との乖離を測る
- 1 click では一般化しない。**複数サンプル (= 5-10 click)** で挙動を確認してから判断する

### 2. Apple 公式 doc を必ず引く

AVFoundation 系の問題に当たったら、最低限以下を WebFetch で取得 / 確認する:

- 該当 API の Apple Developer Documentation (= `https://developer.apple.com/documentation/avfoundation/...`)
- WWDC sessions (= 「AVPlayer best practices」「Advances in AVPlayer」等の年度別 session)
- AVPlayer / AVPlayerItem の Sample Code

経験則 / Stack Overflow ベースで進めると、Apple が後から仕様を変えた時に振り回される。

## 落とし穴カタログ

### A. `AVPlayer.seek(to:tolerance:)` の I-frame 着地は **実装依存**

- tolerance > 0 の場合、AVPlayer は target ± tolerance の範囲内の I-frame を選ぶ
- **「最近接 I-frame に必ず着地」と保証されているわけではない**。Apple 実装依存で、
  I-frame 配置 / 再生中の最適化 / playback rate 等で変わる
- 観測例 (実機ログ、2026-05-09):
  - 同じ tolerance=5s で、ある click では `landedMinusTarget=21ms` (= ジャスト)、
    別 click では `landedMinusTarget=-712ms` (= 0.7 秒ズレ) と挙動が分かれた
- **対策**: tolerance を小さくする (= frame-accurate に近づく) と着地は安定するが、
  seek 速度が遅くなる + decoder 負荷が上がる

### B. `AVPlayerItemVideoOutput.copyPixelBuffer` の `itemTimeForDisplay` は GOP 由来

- `forItemTime` で要求した時刻に対して、`itemTimeForDisplay` は **「`forItemTime` 以前で
  取得可能な最も近い frame の時刻」** を返す
- AVPlayer が target=4551.513s に正確に seek しても、`itemTimeForDisplay=4551.233s`
  (= 着地点 -280ms = GOP 内の前 I-frame) が返ることがある
- これは **AVPlayerLayer が表示している frame の時刻** とほぼ一致する
- **重要**: `AVPlayer.currentTime()` と「実際に画面に表示されている frame の時刻」は
  別物。click seek が「動かなかったように見える」原因はここに集約することが多い

### C. `AVPlayer.currentTime()` は Main thread から呼ぶと deadlock しうる

- macOS で観測例: Main thread から `avPlayer.currentTime()` を呼ぶと MediaToolbox 内部の
  pthread_mutex に詰まって UI 完全停止
- 対策: `addPeriodicTimeObserver` の closure 内で cached value を保持し、Main thread
  からは cached を参照する
- 詳細は VLCMultiVideoPlayer プロジェクトの `AVFoundationVideoPlayer.swift` の
  `currentTime` プロパティ周辺コメント参照

### D. AVPlayerItemVideoOutput を attach すると pipeline が変わる

- Apple 公式仕様には明示されていないが、実装上 `AVPlayerItemVideoOutput` が attach されて
  いると AVPlayer pipeline が **frame display を駆動する mode** になる
- 永続 attach を撤廃すると、シークバー drag のコマ送り (= frame-stepping scrub) が動か
  なくなる事例あり (VLCMultiVideoPlayer の commit `c845249` で確認)
- **対策**: drag scrub を機能させたいなら永続 attach を維持する。通常再生中の負荷が
  気になる場合は probe lifetime に scope 限定したくなるが、scrub 機能と両立しない

### E. KVO observer / completion handler は任意スレッドで fire

- `AVPlayerItem.status` / `AVPlayer.timeControlStatus` 等の KVO callback、
  `AVPlayer.seek(to:completionHandler:)` の completion は **任意スレッド** で fire する
- Main thread でない可能性が常にある
- 対策: closure 内で `Task { @MainActor in [weak self] ... }` で MainActor へ hop する
- 例外: `addPeriodicTimeObserver(forInterval:queue:using:)` で `queue: .main` を指定した
  closure は確実に main thread で fire する → `MainActor.assumeIsolated` で hop なし可

### F. `addPeriodicTimeObserver` は seek 中も fire する

- seek の途中の中間時刻も periodic observer に流れてくる
- seek completion 時の cached currentTime は **「最後の periodic tick」の値** で、
  AVPlayer 実着地点と乖離していることがある (`completionCachedAgeMs` で測ること)

### G. `automaticallyWaitsToMinimizeStalling = true` (default) の挙動

- buffer underrun 検知時に AVPlayer 内部で `rate=0` で待機 → buffer 戻り → resume
- rate ≥ 2.5x で buffer 消費が早いと underrun → pause → resume の発振が「コマ送り」
  として観測される **可能性がある** (= ただし確定していない、観測例では rate と無関係に
  発生したケースあり)
- `false` に設定しても改善しない事例も観測されている (VLCMultiVideoPlayer issue 332 Step 2)

## Debugging チェックリスト (= 修正前に通す)

映像 / 再生問題が出たら、修正コードを書く前に以下を確認する:

- [ ] **問題はユーザーが見ている "何"?**: 映像 / UI / 音声 / 時間表示 のどれか特定
- [ ] **複数サンプルで再現する**: 5-10 click / drag で挙動が一貫するか確認
- [ ] **`display` field を log に出している?**: `AVPlayerItemVideoOutput.copyPixelBuffer`
  の `itemTimeForDisplay` を log に追加。`current` (= playback clock) と乖離しているか
- [ ] **Apple 公式 doc を引いた?**: 該当 API の WebFetch + WWDC session 確認
- [ ] **Instruments で AVPlayerLayer を観測した?**: Core Animation ツールで表示更新を見る
- [ ] **既存の落とし穴カタログ (上記) に該当しないか?**: 1 つでも該当したら推測の前に検証
- [ ] **修正方針はユーザー意図と合致?**: 「ずらして再生でいい」など曖昧な指示は 1 確認入れる

## 過去事例: VLCMultiVideoPlayer の click seek 問題 (2026-05-09)

ユーザー報告: 「シークバーを click すると一瞬戻ってから同じ位置で再生される」

### 失敗した推測 (= 3 回 revert)

1. snap back 対策 (= cached value 渡し) → UI 段差消したが体感変わらず
2. seek 中 pause + 250ms delay → AVPlayer 内部で paused → playing 遷移したが体感悪化
3. click seek tolerance 分離 (= 0.5s) → AVPlayer は target ジャスト着地するが体感変わらず

### 真の原因 (= 最後の最後で判明)

- AVPlayer は target に正確に着地 (`landedMinusTarget=0ms`)
- しかし `AVPlayerLayer` が表示する frame は **GOP 内の前 I-frame** (= `display=4551.233s`、
  着地 4551.513s から -280ms 前)
- これは **落とし穴カタログ B** に該当
- log には最初から `display` field が出ていたが、私 (Claude) は最後まで見落とした

### 学び

- log の **数値だけ** で原因推定すると、表示パイプラインの別側面 (= GOP / itemTimeForDisplay)
  を見落とす
- `AVPlayer.currentTime` と「実表示 frame」は別物
- Apple 公式 doc を引いていれば `itemTimeForDisplay` の意味は最初から分かった
- 詳細は VLCMultiVideoPlayer プロジェクトの `issues/333-bug-click-seek-perceived-no-op.md`

## 関連プロジェクト / リソース

- VLCMultiVideoPlayer: `~/src/my-products/apps/vlc-multi-video-player/`
  - `VLCMultiVideoPlayer/VideoPlayer/AVFoundationVideoPlayer.swift` (= AVPlayer 実装本体)
  - `VLCMultiVideoPlayer/VideoPlayer/AVPlayerSeekDiagnostics.swift` (= 計測 log)
  - `issues/332-bug-high-rate-playback-frame-stutter.md` (= rate ≥ 2.5x の stutter 問題)
  - `issues/333-bug-click-seek-perceived-no-op.md` (= click seek 体感問題、本 skill の主要事例)

- Apple Developer Documentation:
  - AVPlayer: https://developer.apple.com/documentation/avfoundation/avplayer
  - AVPlayerItem: https://developer.apple.com/documentation/avfoundation/avplayeritem
  - AVPlayerItemVideoOutput: https://developer.apple.com/documentation/avfoundation/avplayeritemvideooutput

- 関連 skill:
  - `swift-vlc-player`: **削除済み** (= VLCKit 専用、本プロジェクトでは未使用)
  - `ios-app-developer`: iOS 全般 (= AVFoundation 個別の落とし穴は本 skill 側に集約)
  - `crash-log-analyzer`: AVPlayer 系 crash の解析

## 改訂履歴

- 2026-05-09: 初版。VLCMultiVideoPlayer の issue 333 (click seek 体感問題) の learning を
  落とし穴 B (= itemTimeForDisplay の GOP 由来挙動) として収録。`swift-vlc-player` skill を
  削除して本 skill に置き換え。
