package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// PMStateFile は dispatcher が起こした PM の様子 (issue 437)。書き手は dispatcher だけ (記録と同じ。426 の決定 1)。
const PMStateFile = "pm.json"

// PMState は PMStateFile の中身。PM の session id と pid は起動の記録 (live の sessions.json。カード ID は dispatcher.PMCardID) に置き、ここには持たない。
type PMState struct {
	Session string `json:"session,omitempty"` // 今の PM の短い id (claude --bg が返したもの)
	Name    string `json:"name,omitempty"`    // 最後に起動したときの worktree と session の名前
	// Launching / LaunchedAt は起動・再開を始めて、結果をまだ確かめていない印 ("起動" / "再開") と、最後に起動・再開を始めた時刻。
	// 起動の前に書く (claude が失敗と返しても立っていることがあり、dispatcher が途中で落ちることもある。次の Tick が一覧で確かめる)
	Launching  string    `json:"launching,omitempty"`
	LaunchedAt time.Time `json:"launchedAt,omitzero"`
	// Telling は印と一緒に渡している最中のカード。取り込めたら Told へ移す (取り込めなければ、次の起動・再開でまた渡す)
	Telling []string `json:"telling,omitempty"`
	// Told は今の PM に知らせ済みで、まだ残っている物の鍵 (依頼の列のカードはカード ID、PG の質問はカード ID@質問待ちに入った時刻。
	// dispatcher.pmKey。列を離れたら外す)。Telling も同じ鍵
	Told []string `json:"told,omitempty"`
	// DeadSince は PM の session が一覧に無い / pid 無し (落ちて自動の再開を待っている) のを最初に見た時刻。生きているのを見たら外す
	DeadSince time.Time `json:"deadSince,omitzero"`
	// Revivals は生きていない PM を起こし直した時刻 (落ち続ける PM を起こし直し続けない。crash の窓を過ぎたものは dispatcher が外す)
	Revivals []time.Time `json:"revivals,omitempty"`
	// Stopped は pro-con の終了で dispatcher が PM を止めた印 (次は自動の再開を待たずに再開する)。起動・再開で外す
	Stopped bool `json:"stopped,omitempty"`
}

// LoadPM は PM の様子を読む。無ければゼロ値 (まだ 1 度も起こしていない)。壊れていたらエラー (ゼロ値と区別する: 起こし直すと 2 本立つ)。
func LoadPM(dir string) (PMState, error) {
	data, err := os.ReadFile(filepath.Join(dir, PMStateFile))
	if errors.Is(err, os.ErrNotExist) {
		return PMState{}, nil
	}
	if err != nil {
		return PMState{}, err
	}
	var s PMState
	if err := json.Unmarshal(data, &s); err != nil {
		return PMState{}, fmt.Errorf("PM の様子 (%s) を読めない: %w", filepath.Join(dir, PMStateFile), err)
	}
	return s, nil
}

// SavePM は PM の様子を書く (書きかけを読ませない)。
func SavePM(dir string, s PMState) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	return writeAtomic(filepath.Join(dir, PMStateFile), data)
}
