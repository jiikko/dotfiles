package runner

import (
	"fmt"
	"strings"
	"time"
)

// transitionTimer は進捗板の段ごとの所要時間を測り、板が成功で閉じたときの 1 行を作る。
// Model は I/O を持たない (時計も読まない) ので、actor が遷移の前後の Transition を渡して測る。
type transitionTimer struct {
	now          func() time.Time
	active       bool
	started      time.Time
	stageStarted time.Time
	stages       []stageTime
}

type stageTime struct {
	stage TransitionStage
	took  time.Duration
}

func (t *transitionTimer) clock() time.Time {
	if t.now != nil {
		return t.now()
	}
	return time.Now()
}

func (t *transitionTimer) begin() {
	now := t.clock()
	t.active, t.started, t.stageStarted, t.stages = true, now, now, nil
}

// observe は 1 回の遷移の前後を受け取る。起動・起動の確認の段で板が閉じ、結果 (ビルド失敗・起動未確認) が無く、
// 子が動いているなら起動・再起動の所要時間の 1 行を返す。それ以外は "" (message を書き換えない)。
func (t *transitionTimer) observe(before, after Transition, state State) string {
	switch {
	case !before.Active && after.Active:
		t.begin()
	case !t.active:
		// 板が model を直接書き換える経路で閉じた後など、測り始めていない板は数えない
	case before.Active && after.Active && before.Stage != after.Stage:
		t.lap(before.Stage)
	case before.Active && !after.Active:
		t.lap(before.Stage)
		t.active = false
		// 成功は子が起動した後 (起動・起動の確認の段) に閉じたときだけ。終了の段で閉じるのは停止の取り消し・
		// 停止コマンドの失敗で、そのときの message (理由) を上書きしない
		if after.Result != TransitionResultNone || state != Running || (before.Stage != TransitionLaunch && before.Stage != TransitionReady) {
			return ""
		}
		switch after.Kind {
		case TransitionRestart:
			return t.summary("再起動しました")
		case TransitionStartup:
			return t.summary("起動しました")
		}
	}
	return ""
}

func (t *transitionTimer) lap(stage TransitionStage) {
	now := t.clock()
	took := now.Sub(t.stageStarted)
	t.stageStarted = now
	t.stages = append(t.stages, stageTime{stage: stage, took: took})
}

func (t *transitionTimer) summary(head string) string {
	parts := make([]string, 0, len(t.stages))
	for _, s := range t.stages {
		parts = append(parts, fmt.Sprintf("%s %s", stageLabel(s.stage), formatSeconds(s.took)))
	}
	return fmt.Sprintf("%s (計 %s: %s)", head, formatSeconds(t.clock().Sub(t.started)), strings.Join(parts, " / "))
}

// stageLabel は進捗板の段の表記 (ui の transitionSteps) と揃える。
func stageLabel(stage TransitionStage) string {
	switch stage {
	case TransitionStop:
		return "終了"
	case TransitionBuild:
		return "ビルド"
	case TransitionLaunch:
		return "起動"
	case TransitionReady:
		return "起動の確認"
	default:
		return string(stage)
	}
}

func formatSeconds(d time.Duration) string {
	return fmt.Sprintf("%.1fs", d.Seconds())
}
