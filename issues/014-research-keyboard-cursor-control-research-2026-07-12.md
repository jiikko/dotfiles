# キーボードによるマウスカーソル操作の調査 — Karabiner D+モードのテコ入れと代替手段（2026-07-12）

調査日: 2026-07-12
目的: 現行の Karabiner-Elements「Mouse Keys Mode v4」（D ホールド + hjkl）のカーソル移動が固定速度で粗い問題に対し、(1) Karabiner 内での滑らかさ改善、(2) キーボードだけで完結する別方式のカーソル操作、の両方向を網羅調査する。
調査方法: Workflow で 5 角度（Karabiner 仕様深掘り / ヒント系 / グリッド系 / 連続移動系 / OS 標準・ファームウェア）の並列 Web 調査 → 網羅性チェックで 4 件補完 → 主要候補 14 件（§3/§4 の実在ツール・仕様主張を優先選定）を一次情報（公式サイト / GitHub / 公式 PDF）で裏取り。加えて根幹の主張（20ms tick・乗算仕様・neru / mouseless の現況）は main agent が GitHub API / raw ソースで直接再確認済み。

---

## 結論（要約）

1. **「滑らかでない」の根本原因は Karabiner の構造的制約であり、config チューニングでは解消できない。**
   `mouse_key` は 20ms（50Hz）固定間隔のタイマーで等速の相対移動 HID レポートを送り続ける実装で、押下時間に応じた加速カーブがソース上存在しない（`mouse_key_handler.hpp` で確認）。加速の要望 issue [#1907](https://github.com/pqrs-org/Karabiner-Elements/issues/1907) は 2019 年から放置（closed/stale、2026-01 にもコメントが付くが未実装）。Karabiner 内でできるのは「離散的な多段速度」まで。
2. **推奨は 3 段構え。**
   - **(a) 即効・無料**: Karabiner 内チューニング（速度段の追加 + Mouse Key XY Scale + `relative_to: focused_window` グリッド）→ §2
   - **(b) 本命**: 大距離移動をヒント/グリッド系ツールに委譲し、D+モードは微調整・スクロール専用に縮退させる。無料 OSS なら **neru**（活発・多モード統合）、買い切りなら **mouseless**（$20）/ **Homerow**（$49.99）→ §3
   - **(c) 連続移動に本物の加速が欲しい場合**: **Keymou**（$4.99、押下時間で加速）か Hammerspoon 自作 → §4
3. **OS 標準の Mouse Keys（アクセシビリティ）には本物の加速カーブがある**（Initial Delay / Maximum Speed）。Karabiner で hjkl→テンキーに写像して OS 側に加速させる合わせ技は理論上可能だが**未検証の仮説** → §5

---

## 1. 現状整理と不満の根本原因

現行構成（`mac/karabiner.json` "Mouse Keys Mode v4 (rev 3)"）:

- D ホールド + h/j/k/l 同時押し（strict order）でモード有効化、D リリースで解除
- h/j/k/l: `mouse_key` x/y = ±1536（固定速度）、f ホールド: ×2.0 / g ホールド: ×0.3
- s ホールド: スクロール（wheel ±32）、v/n/b: 左/右/中クリック
- u/i/y/6/7/8: `set_mouse_cursor_position` で画面 3x2 グリッドへワープ

### 根本原因（ソースレベルで確認済み）

| 事実 | 出典 |
|---|---|
| `mouse_key` は **20ms 固定間隔**のタイマーで相対 HID レポートを送出。押下時間による加速はどこにも実装されていない | [mouse_key_handler.hpp](https://github.com/pqrs-org/Karabiner-Elements/blob/main/src/share/manipulator/manipulators/post_event_to_virtual_devices/mouse_key_handler.hpp)（`std::chrono::milliseconds(20)`） |
| 複数の `mouse_key` 同時押しの合成は x/y が**加算**、`speed_multiplier` は**乗算**（`speed_multiplier_ *= other.speed_multiplier_`） | [mouse_key.hpp](https://github.com/pqrs-org/Karabiner-Elements/blob/main/src/share/types/mouse_key.hpp) |
| 値は `count_converter`（閾値 128 の端数蓄積器）で int8 の HID 相対移動量に変換。**低い multiplier ほど複数 tick 分をまとめた間欠的な動きになりやすい**（※ソース構造からの推論、実測未検証） | 同 mouse_key_handler.hpp |
| 出力は仮想 HID マウスへの生の相対値で、公式 docs も「速度は System Settings の Mouse 設定に依存」と明記。**macOS 側のポインタ設定（追跡速度）の影響を後段で受ける**（issue [#3556](https://github.com/pqrs-org/Karabiner-Elements/issues/3556) は stale close、変更予定なし。加速カーブがどこまで効くかの実測は未実施） | [公式 docs](https://karabiner-elements.pqrs.org/docs/json/complex-modifications-manipulator-definition/to/mouse-key/) |
| 方向キー自体に `speed_multiplier` を持たせると対角同時押しで乗算が爆発する既知の失敗パターンあり（issue [#3049](https://github.com/pqrs-org/Karabiner-Elements/issues/3049)。オーバーフロー自体は v15.4.0 (2025-06-29) で修正）。**現行構成（方向キー = x/y のみ、f/g = multiplier のみ）はこの教訓どおりの正しい設計で変更不要** | 同 issue / NEWS.md |

Karabiner-Elements 本体は v16.1.0（2026-07-05）まで活発にメンテされており、macOS 15 Sequoia / 26 Tahoe とも公式サポート対象。乗り換え理由はない。

---

## 2. 方向性 A: Karabiner 内でのテコ入れ（できること・できないこと）

**できないこと**: 押下時間に応じた連続加速（上記のとおり実装が存在しない）。

**できること**:

1. **速度段の追加** — x/y を持たない multiplier 専用キーを 1〜2 段追加する（例: 精密 0.15 / 長距離 4.0）。乗算合成なので f(2.0)+新キー(4.0) 同時押しで 8.0 のような重ね掛けも効く（意図しない組み合わせに注意）。
2. **Mouse Key XY Scale** — Settings > Virtual Keyboard タブの「Mouse Key XY speed」（%指定）が全 mouse_key 出力に乗る独立レバー。JSON の multiplier を刻む前に、まずここで基準速度を決めると調整しやすい。※Devices タブの「XY movement multiplier」は物理マウス用で mouse_key には効かない別物。
3. **macOS 側の追跡速度** — System Settings のトラックパッド/マウス「軌跡の速さ」も mouse_key の見かけ速度に効く（公式 docs 記載の依存関係。効き方の詳細は未実測）。
4. **グリッドワープの強化** — v16.0.0（2026-05-03）で `set_mouse_cursor_position` に `relative_to: focused_window` が追加された。**画面全体ではなくフォーカス中ウィンドウ内の相対グリッド**へワープできる。現行 3x2 の画面グリッドに加えて「ウィンドウ内 3x3」を別キーに割ると実用的な到達精度が上がる。なお相対移動（現在位置からのオフセット）は非対応（point/percent の絶対指定のみ、ソースで確認）。
5. **to_if_held_down による 2 段階疑似加速（未検証の設計案）** — 押下直後は低速 mouse_key、閾値（例 300ms）超えで高速版に切り替える案。プリミティブは全て実在するが、**この組み合わせを実装した公開設定例は公式ギャラリー含め発見できず、動作未確認**。実装しても離散的な 2〜3 段であり連続カーブにはならない。
6. **精密モードのカクつき対策（推論ベース）** — g の 0.3 をさらに下げるより、XY Scale で基準を下げて g は 0.5〜0.7 に留める方が count_converter の蓄積が滑らかに働きやすいと考えられる（※未実測）。

**評価**: 数十分で試せる改善だが、得られるのは「離散段の増加」まで。「滑らかさ」の質的改善は構造的に不可能。

---

## 3. 方向性 B: ヒント/グリッド系 — カーソル移動そのものをなくす（本命）

「目的地までスムーズに動かす」のではなく「目的地に一撃で飛ぶ」方式。ターミナル + Neovim 中心でマウスが必要になる残りの場面（ブラウザ・GUI アプリの散発クリック）とは相性が良い。

| ツール | 方式 | 価格 | メンテ状況（2026-07 時点） | 特記 |
|---|---|---|---|---|
| **[neru](https://github.com/y3owk1n/neru)** ⭐推奨 | Recursive Grid / Grid / Hints（AX API + Vision OCR）/ vim 風スクロールを 1 本に統合 | 無料 (MIT) | **v1.46.1 (2026-07-08)、ほぼ日次で開発中**。Go 製、約 450 stars | brew tap で導入可。全キーバインドリマップ可。warpd の実質的後継。過去に brew の `depends_on` 誤解釈問題 (#984)・権限再認識問題 (#736) あり、いずれも解決済み |
| **[mouseless](https://mouseless.click/)** | 画面全体 2 文字グリッド + Free mode（連続移動・S/D/F ホールドで段階加速・スクロール・ドラッグ） | $20 買い切り / Setapp（7 日試用） | **v1.0.0 安定版 (2026-07-07)**。本体クローズド、issue トラッカー 723 stars | `brew install --cask mouseless`（macOS ≥12、cask で確認済み）。グリッド+連続移動の両輪が 1 本で揃う。Sequoia+M4 で「途中からトリガーしなくなる」未解決 issue #515 あり |
| **[Homerow](https://www.homerow.app/)** | 検索バー + UI 要素ラベル（Vimac 後継）。Shift+Cmd+J でスクロールモード | $49.99 買い切り。試用は**期間無制限**（50 起動ごとに購入リマインダー） | v1.5.3 (2026-03-19)。Tahoe 対応修正済み (v1.4.1) | macOS 13+。操作は 2 段階（検索 or Tab/Shift+ラベルでフォーカス → Return でクリック）。商用では完成度最有力だが高価格帯 |
| **[Scoot](https://github.com/mjrusso/scoot)** | element（avy 風 2 文字）/ grid / freestyle（連続移動）の **3 モードを手動切替**（自動フォールバックではない） | 無料 (BSD-3) | v1.3 (2026-03-02)。README の対応 OS 表記（Big Sur/Monterey）は古いまま | `brew install --cask scoot`。Sequoia で原因不明ビープ (#51 open)、Tahoe+スケーリング解像度でクラッシュ (#59 open) の固有バグ報告あり |
| **[Shortcat](https://shortcat.app/)** | fuzzy 検索で UI 要素を絞り込み（ラベル暗記不要） | 無料（「延長ベータ」扱い、正式な価格体系は未定） | v0.12.2 (2025-07-03) 以降 **約 1 年リリースなし** | macOS 13+、「tested on 15.3」と明記。Electron 対応を公式に明記（VS Code 等）。停滞リスクあり |
| **[Wooshy](https://wooshy.app/)** | 全 UI（Dock・メニューバー・ステータスバー含む）をテキスト検索してクリック | サブスク $3.28/月（年額 $39.28 = 実質無割引）。無料版は「ランダムに休止」 | Ws48 (2026-04-28)、ほぼ月次リリース。Tahoe 対応明記 (Ws44/45) | 最小要件 Sonoma 14.6+ |
| **[Superkey](https://superkey.app/)** | **OCR 主体**（+ オプトインで AX API 併用）のテキスト検索クリック + Hyperkey 統合 | 買い切り（価格は一次情報で未確認。第三者サイトに $15.99 の記載）。20 日試用 | v1.66 (2026-06-23)、活発 | macOS 12+。OCR 方式なので AX ツリーが貧弱なアプリでも文字が見えれば効きうる（公式明言はなし） |
| **[Mousio](https://github.com/jaywcjlove/mousio)** (+ Mousio Hint) | 象限グリッド再帰分割 + カーソルモード + Hint 連携 | 本体無料 + IAP（$0.99/月 or $6.99 買い切り）。Hint は brew cask 配布（価格情報なし） | 本体 v2.3.0 (2025-12-13)、リポジトリは 2026 年も活動 | App Store 配布（macOS 14+）。**両方ともクローズドソース**（GitHub は issue 窓口のみ） |
| [stochos](https://github.com/museslabs/stochos) | 2 文字 400 セルグリッド + サブグリッド絞り込み | 無料 (GPL-3.0) | v1.0.0 (2026-07-07)、新興 | **macOS はシングルディスプレイのみ対応**。brew なし |
| [KeyboardStack](https://keyboardstack.com/) | グリッドズームイン + カーソルモード | 無料版あり / フル $29.99 | 更新状況を示す一次情報が見つからず未確認 | macOS 13.5+ |
| [warpd](https://github.com/rvaiya/warpd) (hint/grid) | grid 二分探索 + hint | 無料 (MIT) | **実質メンテ停止**（最終コミット 2023-06-03、macOS 15.6 ビルド修正 PR #329 が 7 ヶ月放置、brew なし） | 非推奨。§4 の加速実装は参考価値あり |
| Vimac | （Homerow の前身） | — | 2021 年に実装終了、配布サイトもダウン | 新規導入不可。Homerow へ |

**共通の注意**: 全ツールとも日本語 UI / 日本語 IME 経由入力との相性は公式情報が無く未確認。ヒント系の精度は対象アプリの Accessibility 実装に依存する。

---

## 4. 方向性 C: 連続移動エンジンの置き換え — 本物の加速カーブ

| 手段 | 加速 | 価格 | 評価 |
|---|---|---|---|
| **[Keymou](https://manytricks.com/keymou/)** (Many Tricks) | ホットキー押下中、押下時間に応じて加速（公式ヘルプ明記。カーブ形状は非公開） | $4.99（App Store。公式サイトは $5 表記）買い切り、1000 回まで無料デモ、60 日返金保証 | 既製品で最安・最手軽。Move のほか Move by Division（十字線の**二分探索**方式）・エッジ/コーナー移動・スクロール・クリックも同梱。ただし v1.2.11 (2025-02-14) が最新で、実質的な機能更新は 2022 年が最後。Sequoia 対応の公式明言なし（macOS 10.13+ 表記のみ） |
| **Hammerspoon 自作**（参考: [mousekeys.spoon](https://github.com/eishexac/mousekeys)） | `speed = initialSpeed + acceleration × holdTime`、約 60fps、サブピクセル蓄積、delta-time 補正 | 無料 (MIT) | 今回のテーマに最も直接合致する実装。ただし **star 0・コミット 10・作者 1 人の個人実験**で、そのまま採用ではなく参考実装として読んで自作する想定が現実的。`remapKey = nil` 設定で Karabiner 側のモードキー（D レイヤー）と分業できる設計。Hammerspoon 本体は活発（v1.1.1, 2026-02-26）だが Sequoia/Tahoe での間欠フリーズ issue [#3831](https://github.com/Hammerspoon/hammerspoon/issues/3831) が open（原因は `hs.window.filter` / `hs.execute` 系との推定で、eventtap+timer 構成には直接該当しない可能性が高い） |
| [warpd](https://github.com/rvaiya/warpd) normal mode | **ソースで確認済みの本物の物理モデル**（`v += elapsed × a`、初速 220px/s・加速 700px/s²・上限 1600px/s を独立指定、accelerator/decelerator キーも標準搭載） | 無料 (MIT) | 理論上は理想形だが前述のとおりメンテ停止・Sequoia でビルド不可の可能性（PR #329 未マージ）・署名なし配布。**加速パラメータ設計の参考資料**としての価値が主 |
| [mousemaster](https://github.com/petoncle/mousemaster) | initial-velocity / max-velocity / acceleration / acceleration-easing（linear/quadratic/smoothstep 等）を設定可能 | LICENSE ファイルなし（形式上 OSS と言えない） | **Windows 専用**（README 明記）。macOS 対応 PR #68 が 2026-07-11 にオープンされたがレビュー中・未完成。「連続移動+ヒント+グリッドを 1 ツールでコンボキー切替」という設計は参考価値大。ウォッチ対象 |
| [cliclick](https://github.com/BlueM/cliclick) | 2 点間 easing のみ | 無料 | 「押しっぱなしで加速」には構造的に不向き（都度プロセス起動）。最終リリース 2022 年。対象外 |

---

## 5. 方向性 D: OS 標準機能

- **macOS Mouse Keys**（System Settings > アクセシビリティ > ポインタコントロール）: **Initial Delay と Maximum Speed の 2 スライダーを持つ、本物の加速付きカーソル移動**。無料・Apple がメンテ。
  - **未検証の仮説**: Karabiner で D レイヤー中の hjkl をテンキー（kp4/kp2/kp8/kp6 等）に写像し、OS 側 Mouse Keys を常時 ON にしておけば、「D+hjkl の UX のまま OS の加速カーブに乗る」可能性がある。Karabiner 仮想キーボードのキーイベントを OS の Mouse Keys が拾うかは一次情報が見つからず、**実機検証が必要**（検証手順: Mouse Keys を ON → `karabiner_cli` か簡易ルールで j→kp2 を一時マップ → 押しっぱなしで加速するか確認）。拾わなければこの案は不成立。
  - 制約: 調整は 2 スライダーのみ。現行 macOS では defaults キーによる CLI 調整方法は文書化が見つからない。Mouse Keys ON 中はテンキーが数字入力に使えなくなる等の副作用にも注意。
- **Full Keyboard Access**（Tab ナビゲーション）: OS 標準のフォーカス移動。デフォルトでは対象が限定的で、「All Controls」(Ctrl+F7) が必要。Firefox 非対応・フォーカス迷子などの実務上の穴があり、主役にはならない。
- **Switch Control の Point Mode**（走査線交差方式）: 原理上任意ピクセルに到達できるが、タイマー走査待ちが本質的に低速。参考知識レベル。
- **ブラウザ内の解決**: [Vimium](https://github.com/philc/vimium)（v2.4.2, 2026-03、活発）/ Vimium C / Surfingkeys（Safari 版は App Store 配布あり）のヒントモードで「ブラウザ内のクリック」はカーソル移動なしで潰せる。Safari 派生の Vimari は約 5 年停滞、Hammerspoon 実装の [vifari](https://github.com/dzirtusss/vifari) も 14 ヶ月更新なし。ヒントで潰せない残りはドラッグ&ドロップ / canvas 系 UI（Figma・地図）/ 動画スクラブ等で、そこは連続移動系（§4）か D+モードの守備範囲。

---

## 6. 方向性 E: キーボードファームウェア

- **Advantage360（非 Pro / SmartSet）= 現有機**: 公式 Action Tokens（現行 v3-31-23）に mouse tokens（クリック / スクロール / 上下左右移動）は存在するが、**速度・加速を指定するパラメータが仕様上一切ない**。さらに公式マニュアルに「Karabiner 等のキーボードカスタマイズソフト併用で予測不能な挙動になりうる」という注意書きあり。**滑らかさ目的で乗り換える動機なし**。
- **Advantage360 Pro（ZMK）**: `&mmv` + `acceleration-exponent` / `time-to-max-speed-ms` で**ファームウェアレベルの加速カーブ**を構成可能。Kinesis 公式フォークにもマウス移動サポート追加済み。ホスト OS 非依存で効く根本解だが、**Pro への買い替えが前提**（本体 $499〜の再投資）。将来キーボードを更新する際の判断材料。
- QMK の kinetic mouse keys も同等機能を持つ（参考。現有機では使えない）。

---

## 7. 推奨アクションプラン

1. **［無料・即日］neru を試す** — `brew install --cask y3owk1n/tap/neru`（公式 README 記載のコマンドを確認済み）。大距離移動 + クリックを Recursive Grid / Hints に委譲し、D+モードは微調整・スクロール用に残す。合わなければ mouseless（$20、グリッド + 加速付き Free mode が 1 本で揃う）→ Homerow（$49.99、完成度最優先）の順に試用。
2. **［無料・並行実施］Karabiner チューニング** — §2 の 1〜4（速度段追加 / XY Scale / focused_window グリッド）。5 の to_if_held_down 2 段加速は実験枠。
3. **［約 $5］連続移動の加速がどうしても欲しければ Keymou** — 1000 回無料デモで体感確認。Karabiner D+モードとホットキーが重ならないよう整理が必要。
4. **［実験枠］hjkl→テンキー写像 + OS Mouse Keys 仮説の実機検証**（§5）。成立すれば「追加ツールなしで本物の加速」という最小構成になる。
5. **［長期］** mousemaster の macOS 対応（PR #68）をウォッチ。キーボード買い替え時は Adv360 Pro (ZMK) の加速付き mouse keys を判断材料に。

## 8. 未検証事項・調査の限界

- 日本語 IME との干渉（ヒント系の検索入力・ホットキー捕捉との両立）は全ツール未確認
- to_if_held_down 2 段加速案は机上設計（公開実例なし・動作未確認）
- OS Mouse Keys × Karabiner 仮想キーの連携可否は未検証の仮説
- count_converter による低速時の間欠感はソース構造からの推論（実測なし）
- macOS 15 明示対応が一次情報で確認できたのは Homerow / Shortcat / Wooshy / Scoot（実利用報告）/ Mousio（要件上）程度。他は「直近まで開発が続いている」ことからの推測
- 各ツールと Karabiner D+モードの同時使用時のキー競合は個別検証していない
- 情報はすべて 2026-07-12 時点
