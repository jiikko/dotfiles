package health

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"doctor/runner"
	"doctor/ssd"
)

const (
	batteryCycleWarn = 1000 // Apple の MacBook の電池が設計上もつとしている回数
	batteryCapWarn   = 80   // 最大容量 (%) がこれ未満 = 交換の目安
	panicWindow      = 30 * 24 * time.Hour
	crashRepeat      = 3 // 同じプロセスが直近 7 日にこの回数以上落ちたら注意
	uptimeLong       = 45 * 24 * time.Hour
)

func (s *scanner) hw(ctx context.Context) []Check {
	cs := []Check{s.model(ctx), s.osVersion(ctx), s.uptime(ctx)}
	cs = append(cs, s.battery(ctx)...)
	cs = append(cs, s.ssd(ctx)...)
	cs = append(cs, s.thermal(ctx), s.panics(), s.crashes())
	return cs
}

// model は機種・チップ・メモリ。発売年とサポートの区分 (ビンテージ / オブソリート) は手元に情報源が無いので、
// skill が機種 ID で Web を引く。シリアル番号・UUID は読まない (構造体に欄を置かない)。
func (s *scanner) model(ctx context.Context) Check {
	const id, label = "hw.model", "機種"
	out, stderr, rc, err := s.cmd(ctx, 30*time.Second, "/usr/sbin/system_profiler", "-json", "SPHardwareDataType")
	if err != nil || rc != 0 {
		return unknown(id, label, cmdNote("system_profiler", rc, err, stderr))
	}
	var d struct {
		SP []struct {
			Name   string `json:"machine_name"`
			Model  string `json:"machine_model"`
			Chip   string `json:"chip_type"`
			Memory string `json:"physical_memory"`
		} `json:"SPHardwareDataType"`
	}
	if err := json.Unmarshal([]byte(out), &d); err != nil || len(d.SP) == 0 || d.SP[0].Model == "" {
		return unknown(id, label, "SPHardwareDataType の JSON が読めない")
	}
	h := d.SP[0]
	return Check{ID: id, Label: label, Value: fmt.Sprintf("%s (%s) / %s / メモリ %s", h.Name, h.Model, h.Chip, h.Memory),
		Status: StatusInfo, Note: "発売年・Apple のサポートの区分・最新の macOS の対象かは、機種 ID で調べる"}
}

func (s *scanner) osVersion(ctx context.Context) Check {
	const id, label = "hw.os", "macOS"
	out, stderr, rc, err := s.cmd(ctx, 5*time.Second, "/usr/bin/sw_vers")
	if err != nil || rc != 0 {
		return unknown(id, label, cmdNote("sw_vers", rc, err, stderr))
	}
	kv := map[string]string{}
	for line := range strings.SplitSeq(out, "\n") {
		if k, v, ok := strings.Cut(line, ":"); ok {
			kv[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
	if kv["ProductVersion"] == "" {
		return unknown(id, label, "sw_vers に ProductVersion が無い")
	}
	return Check{ID: id, Label: label, Value: fmt.Sprintf("%s (%s)", kv["ProductVersion"], kv["BuildVersion"]), Status: StatusInfo}
}

func (s *scanner) uptime(ctx context.Context) Check {
	const id, label = "hw.uptime", "起動からの時間"
	out, stderr, rc, err := s.cmd(ctx, 5*time.Second, "/usr/sbin/sysctl", "-n", "kern.boottime")
	if err != nil || rc != 0 {
		return unknown(id, label, cmdNote("sysctl", rc, err, stderr))
	}
	sec, ok := parseBoottime(out)
	if !ok {
		return unknown(id, label, "kern.boottime の sec が読めない")
	}
	up := s.o.Now().Sub(time.Unix(sec, 0))
	c := Check{ID: id, Label: label, Value: fmtDur(up), Status: StatusInfo}
	if up >= uptimeLong {
		c.Note = "長く起動したままだと、回収されないゾンビ・リーク・適用待ちの更新が溜まる。下の項目に異常があれば再起動で消えることが多い"
	}
	return c
}

// parseBoottime は "{ sec = 1791651029, usec = 820770 } Sun Oct 11 ..." の sec を読む。
func parseBoottime(out string) (int64, bool) {
	_, rest, ok := strings.Cut(out, "sec = ")
	if !ok {
		return 0, false
	}
	v, _, _ := strings.Cut(rest, ",")
	n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
	return n, err == nil
}

func (s *scanner) battery(ctx context.Context) []Check {
	const id, label = "hw.battery", "バッテリー"
	out, stderr, rc, err := s.cmd(ctx, 30*time.Second, "/usr/sbin/system_profiler", "-json", "SPPowerDataType")
	if err != nil || rc != 0 {
		return []Check{unknown(id, label, cmdNote("system_profiler", rc, err, stderr))}
	}
	b, present, err := parseBattery(out)
	if err != nil {
		return []Check{unknown(id, label, "SPPowerDataType の JSON が読めない: "+err.Error())}
	}
	if !present {
		return []Check{{ID: id, Label: label, Value: "無い (デスクトップ)", Status: StatusInfo}}
	}
	c := Check{ID: id, Label: label, Status: StatusOK,
		Value: fmt.Sprintf("状態 %s / 最大容量 %s / 充放電 %d 回", b.Health, b.MaxCap, b.Cycles)}
	var why []string
	if b.Health != "Good" {
		why = append(why, "状態が Good でない ("+b.Health+")")
	}
	if n, err := strconv.Atoi(strings.TrimSuffix(b.MaxCap, "%")); err == nil && n < batteryCapWarn {
		why = append(why, fmt.Sprintf("最大容量が %d%% 未満", batteryCapWarn))
	}
	if b.Cycles >= batteryCycleWarn {
		why = append(why, fmt.Sprintf("充放電が %d 回以上 (設計上の目安)", batteryCycleWarn))
	}
	if len(why) > 0 {
		c.Status, c.Note = StatusWarn, strings.Join(why, " / ")
		c.Fix = "Apple Store / 正規サービスプロバイダでバッテリーを交換する (本体の買い替えより安い。販売終了から最長 10 年まで交換を受け付ける)"
	}
	return []Check{c}
}

type batteryInfo struct {
	Cycles int    `json:"sppower_battery_cycle_count"`
	Health string `json:"sppower_battery_health"`
	MaxCap string `json:"sppower_battery_health_maximum_capacity"`
}

// parseBattery はバッテリーの節 (_name が spbattery_information) を読む。節が無ければデスクトップ (present=false)。
// 節はあるのに健康の欄が無いのは鍵の名前が変わった形なので、「無い」にせず読めないとする。
func parseBattery(out string) (batteryInfo, bool, error) {
	var d struct {
		SP []struct {
			Name   string       `json:"_name"`
			Health *batteryInfo `json:"sppower_battery_health_info"`
		} `json:"SPPowerDataType"`
	}
	if err := json.Unmarshal([]byte(out), &d); err != nil {
		return batteryInfo{}, false, err
	}
	if len(d.SP) == 0 {
		return batteryInfo{}, false, errors.New("SPPowerDataType の節が無い")
	}
	for _, sp := range d.SP {
		if sp.Name != "spbattery_information" {
			continue
		}
		if sp.Health == nil || sp.Health.Health == "" {
			return batteryInfo{}, false, errors.New("バッテリーの節に健康の欄が無い")
		}
		return *sp.Health, true, nil
	}
	return batteryInfo{}, false, nil
}

// ssd は doctor の SSD タブと同じ判定 (doctor/ssd) を、この観点の行に写す。
func (s *scanner) ssd(ctx context.Context) []Check {
	rep := ssd.Scan(ctx, ssd.Options{Run: s.o.Run, LookPath: runner.LookPath, Now: s.o.Now})
	cs := make([]Check, 0, len(rep.Checks))
	for i, c := range rep.Checks {
		hc := Check{ID: fmt.Sprintf("hw.ssd.%d", i), Label: "SSD: " + c.Label, Value: c.Value, Note: c.Note, Status: ssdStatus(c.Status)}
		if c.Label == "smartctl" && rep.SmartctlMissing {
			hc.Fix = ssd.InstallHint
		}
		if hc.Status == StatusBad || hc.Status == StatusWarn {
			hc.Fix = "Time Machine などでバックアップを取ってから、Apple で診断を受ける (内蔵 SSD は交換できず、本体の修理・買い替えになる)"
		}
		cs = append(cs, hc)
	}
	return cs
}

func ssdStatus(st ssd.Status) Status {
	switch st {
	case ssd.StatusOK:
		return StatusOK
	case ssd.StatusWarn:
		return StatusWarn
	case ssd.StatusFail:
		return StatusBad
	case ssd.StatusInfo, ssd.StatusUnchecked:
		return StatusInfo
	case ssd.StatusUnknown:
		return StatusUnknown
	}
	return StatusUnknown
}

func (s *scanner) thermal(ctx context.Context) Check {
	const id, label = "hw.thermal", "熱による性能の制限"
	out, stderr, rc, err := s.cmd(ctx, 5*time.Second, "/usr/bin/pmset", "-g", "therm")
	if err != nil || rc != 0 {
		return unknown(id, label, cmdNote("pmset", rc, err, stderr))
	}
	if strings.Contains(out, "No thermal warning level has been recorded") &&
		strings.Contains(out, "No performance warning level has been recorded") {
		return Check{ID: id, Label: label, Value: "記録なし", Status: StatusOK}
	}
	first, _, _ := strings.Cut(strings.TrimSpace(out), "\n")
	return Check{ID: id, Label: label, Value: first, Status: StatusWarn,
		Note: "熱で性能を落とした記録がある", Fix: "通気口のほこりを払う・柔らかい物の上で使わない・高負荷のプロセスを見る。続くならファンの点検"}
}

// panics は直近 30 日のカーネルパニック。.contents.panic (送信用の付随データ) は同じパニックの写しなので数えない。
func (s *scanner) panics() Check {
	const id, label = "hw.panic", "カーネルパニック (直近 30 日)"
	since := s.o.Now().Add(-panicWindow)
	n, read := 0, 0
	for _, dir := range s.o.DiagDirs {
		ents, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		read++
		for _, e := range ents {
			if !strings.HasSuffix(e.Name(), ".panic") || strings.HasPrefix(e.Name(), ".") {
				continue
			}
			if fi, err := e.Info(); err == nil && !fi.ModTime().Before(since) {
				n++
			}
		}
	}
	if read == 0 {
		return unknown(id, label, "DiagnosticReports を読めない")
	}
	if n == 0 {
		return Check{ID: id, Label: label, Value: "なし", Status: StatusOK}
	}
	return Check{ID: id, Label: label, Value: fmt.Sprintf("%d 回", n), Status: StatusBad,
		Note: "/Library/Logs/DiagnosticReports/*.panic の panicString を見る (zone map exhausted ならカーネルのメモリリーク)",
		Fix:  "原因の行を確かめる。ハードウェア由来が疑われるなら Apple Diagnostics (電源ボタン長押しで起動) を回す"}
}

// crashes は直近 7 日に同じプロセスが繰り返し落ちたもの。
func (s *scanner) crashes() Check {
	const id, label = "hw.crash", "繰り返し落ちているプロセス (直近 7 日)"
	since := s.o.Now().Add(-recentDiag)
	counts := map[string]int{}
	read := 0
	for _, dir := range s.o.DiagDirs {
		ents, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		read++
		for _, e := range ents {
			name := e.Name()
			if !strings.HasSuffix(name, ".ips") || strings.HasPrefix(name, "JetsamEvent-") || strings.HasPrefix(name, "ExcUserFault_") {
				continue
			}
			if fi, err := e.Info(); err == nil && !fi.ModTime().Before(since) {
				counts[diagProcName(name)]++
			}
		}
	}
	if read == 0 {
		return unknown(id, label, "DiagnosticReports を読めない")
	}
	rep := map[string]int{}
	for k, v := range counts {
		if v >= crashRepeat {
			rep[k] = v
		}
	}
	if len(rep) == 0 {
		return Check{ID: id, Label: label, Value: "なし", Status: StatusOK}
	}
	return Check{ID: id, Label: label, Value: joinCounts(rep), Status: StatusWarn,
		Note: "テストがわざと落とすもの (bash の SIGSEGV など) も数える。中身の procPath と親プロセスで仕分ける",
		Fix:  "crash-log-analyzer skill で最新の .ips を読む"}
}
