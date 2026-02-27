# 共通ルール

## Issue管理

- `issue/*.md` の内容に対応した後、作業が完了したら対応するissueファイルを `issue/done/` ディレクトリに移動すること

## 設計方針

- Godクラスを避けること。クラスが肥大化しそうな場合は、意味のある単位（責務ごと）でクラスを分割できないか検討すること
- 変更したファイルにGodクラス/Godファイルの予兆（責務の混在、過度な行数など）を見つけたら、リファクタリングを提案すること
- バグフィックス後、そのプロジェクトに導入されているlinterのカスタムルールやpresetルールで再発防止できないか検討し、提案すること

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
| issue-sync, issue同期, 完了漏れ, done移動 | `~/.claude/skills/issue-sync/SKILL.md` |
