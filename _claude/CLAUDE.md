# 共通ルール

## 作業開始前の準備

- コードを書き始める前に、必ず `git pull` を実行して最新の状態に更新すること

## Issue管理

- `issue/*.md` の内容に対応した後、作業が完了したら対応するissueファイルを `issue/done/` ディレクトリに移動すること

## 設計方針

- Godクラスを避けること。クラスが肥大化しそうな場合は、意味のある単位（責務ごと）でクラスを分割できないか検討すること
- 変更したファイルにGodクラス/Godファイルの予兆（責務の混在、過度な行数など）を見つけたら、リファクタリングを提案すること
- バグフィックス後、そのプロジェクトに導入されているlinterのカスタムルールやpresetルールで再発防止できないか検討し、提案すること

## 不具合対応の原則

不具合対応は「ログを足して現象を追う」より先に、設計上の前提（契約）を見直して構造で潰す。

- まず **不変条件（Invariant）** を言語化する（例：deep link は失われない／同一ファイルの同一性は一意／UI失敗で再生は止まらない）
- **失敗モード**（順序競合・再送・二重実行・部分失敗・再起動）を列挙し、設計で吸収する
- **境界（main/renderer、UI/Domain、外部API）** ごとに責務を分離し、手続きの連鎖ではなく「コマンド＋結果」の形にする
- 同一性は **安定キー（id/path_lower 等）** に統一し、表示用文字列に依存しない
- 追加ログは最後の手段。必要なら「イベント／状態遷移」が観測できる設計にする
- PRでは、修正が「症状への対処」ではなく「前提の是正」になっているかを必ず確認する

## スキルファイル参照

`~/.claude/skills/` に専門知識スキルが格納されている。以下のキーワードに関連するタスクでは、対応する SKILL.md を作業前に Read すること。

| キーワード | 参照先 |
|-----------|-------|
| 監査, audit, コードレビュー全体 | `~/.claude/skills/audit/SKILL.md` |
| コミット, commit, git commit | `~/.claude/skills/c/SKILL.md` |
| forge, 専門家実装, クロスレビュー | `~/.claude/skills/forge/SKILL.md` |
| CSS, Node.js, Electron, フロントエンド, デスクトップアプリ | `~/.claude/skills/frontend-desktop-dev/SKILL.md` |
| iOS, iPhone, XcodeGen, SPM, code signing, AVFoundation, @rpath | `~/.claude/skills/ios-app-developer/SKILL.md` |
| iOS Simulator, シミュレータ, xcrun simctl, UIテスト自動化 | `~/.claude/skills/ios-simulator-skill/SKILL.md` |
| パフォーマンス分析, perf.log, ボトルネック | `~/.claude/skills/perf-analysis/SKILL.md` |
| スモークテスト, smoke test, tt-client, 動作確認 | `~/.claude/skills/smoke-test/SKILL.md` |
| WCAG, アクセシビリティ, ダークモード, スタイルレビュー | `~/.claude/skills/style-review/SKILL.md` |
| VLCKit, MobileVLCKit, VLCMediaPlayer, RTSP, HLS, RTMP, メディア再生 | `~/.claude/skills/swift-vlc-player/SKILL.md` |
| watchOS, Apple Watch, WatchKit, WatchConnectivity, HealthKit, コンプリケーション | `~/.claude/skills/watchos-expert/SKILL.md` |
| App Store, TestFlight, 審査, リジェクト, App Store Connect | agent: `appstore-submission-expert` |
