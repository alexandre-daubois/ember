package fetcher

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"time"

	"github.com/shirou/gopsutil/v4/process"
)

// FindFrankenPHPProcess scans running processes for one whose name or command
// line contains "frankenphp" and returns its PID.
func FindFrankenPHPProcess(ctx context.Context) (int32, error) {
	procs, err := process.ProcessesWithContext(ctx)
	if err != nil {
		return 0, fmt.Errorf("list processes: %w", err)
	}
	for _, p := range procs {
		name, err := p.NameWithContext(ctx)
		if err != nil {
			continue
		}
		if strings.Contains(strings.ToLower(name), "frankenphp") {
			return p.Pid, nil
		}
		cmdline, err := p.CmdlineWithContext(ctx)
		if err != nil {
			continue
		}
		if strings.Contains(strings.ToLower(cmdline), "frankenphp") {
			return p.Pid, nil
		}
	}
	return 0, fmt.Errorf("frankenphp process not found")
}

// FindCaddyProcess scans running processes for one whose name or command line
// contains "caddy" and returns its PID.
func FindCaddyProcess(ctx context.Context) (int32, error) {
	procs, err := process.ProcessesWithContext(ctx)
	if err != nil {
		return 0, fmt.Errorf("list processes: %w", err)
	}
	for _, p := range procs {
		name, err := p.NameWithContext(ctx)
		if err != nil {
			continue
		}
		lower := strings.ToLower(name)
		if strings.Contains(lower, "caddy") && !strings.Contains(lower, "frankenphp") {
			return p.Pid, nil
		}
	}
	return 0, fmt.Errorf("caddy process not found")
}

type processHandle struct {
	proc       *process.Process
	lastCPU    float64
	lastSample time.Time
	numCPU     float64
}

func (h *processHandle) reset() {
	h.proc = nil
	h.lastCPU = 0
	h.lastSample = time.Time{}
}

func newProcessHandle(pid int32) *processHandle {
	h := &processHandle{
		numCPU: float64(runtime.NumCPU()),
	}
	if pid > 0 {
		if p, err := process.NewProcess(pid); err == nil {
			h.proc = p
			if times, err := p.Times(); err == nil {
				h.lastCPU = times.User + times.System
				h.lastSample = time.Now()
			}
		}
	}
	return h
}

func (h *processHandle) fetch(ctx context.Context) (ProcessMetrics, error) {
	if h.proc == nil {
		return ProcessMetrics{}, nil
	}

	times, err := h.proc.TimesWithContext(ctx)
	if err != nil {
		h.reset()
		return ProcessMetrics{}, fmt.Errorf("read cpu times: %w", err)
	}

	memInfo, err := h.proc.MemoryInfoWithContext(ctx)
	if err != nil {
		h.reset()
		return ProcessMetrics{}, fmt.Errorf("read memory info: %w", err)
	}

	createTime, err := h.proc.CreateTimeWithContext(ctx)
	if err != nil {
		h.reset()
		return ProcessMetrics{}, fmt.Errorf("read create time: %w", err)
	}

	now := time.Now()
	currentCPU := times.User + times.System
	elapsed := now.Sub(h.lastSample).Seconds()

	var cpuPercent float64
	if elapsed > 0 && !h.lastSample.IsZero() {
		cpuPercent = (currentCPU - h.lastCPU) / elapsed * 100
		if cpuPercent < 0 {
			cpuPercent = 0
		}
	}

	h.lastCPU = currentCPU
	h.lastSample = now

	return ProcessMetrics{
		PID:        h.proc.Pid,
		CPUPercent: cpuPercent,
		RSS:        memInfo.RSS,
		CreateTime: createTime,
		Uptime:     time.Since(time.UnixMilli(createTime)),
	}, nil
}
