# 676 (feat): 字幕の速さの警告を synth の時点で出す

起票日: 2026-10-08

## 概要

字幕が速くて読み切れない行の警告 (`caption_speed.go` の `warnFastCaptions`。1 秒 7.5 字を超える行) は、今は build が出す。
mp4 の build の後に警告を見て `pause_after` を足すと、撮り直しになる。

## 根拠

2026-10-08 の Mac vs Windows で、build の後に警告が 1 行ずつ出て、mp4 を 2 回撮り直した。

## 対応方針 (案)

- 判定に使うのは各行の開始時刻 (wav の長さ・`lead_in`・`gap`・`pause_after`) で、どれも synth の時点で揃う。
  synth の最後に同じ判定を回して警告を出す (build の警告は残す: synth の後に `pause_after` だけを直した場合も拾うため)
- 開始時刻の計算を build と synth で 2 つ書かない (build の `assemble` が持つ計算を共有する)

## 受け入れ条件

- [ ] 速すぎる行を含む台本で、synth が警告を出す (build を待たない)
- [ ] synth と build の警告が同じ行・同じ値になる (計算を共有していることをテストで固定する)
- [ ] SKILL.md の手順 4 / 6 に書く
