package fetcher

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"time"

	"github.com/shirou/gopsutil/v4/process"
)

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

type processHandle struct {
	proc       *process.Process
	lastCPU    float64
	lastSample time.Time
	numCPU     float64
}

func newProcessHandle(pid int32) (*processHandle, error) {
	if pid <= 0 {
		return nil, nil
	}
	p, err := process.NewProcess(pid)
	if err != nil {
		return nil, fmt.Errorf("process %d: %w", pid, err)
	}

	h := &processHandle{
		proc:   p,
		numCPU: float64(runtime.NumCPU()),
	}

	times, err := p.Times()
	if err == nil {
		h.lastCPU = times.User + times.System
		h.lastSample = time.Now()
	}

	return h, nil
}

func (h *processHandle) fetch(ctx context.Context) (ProcessMetrics, error) {
	if h == nil || h.proc == nil {
		return ProcessMetrics{}, nil
	}

	// compute CPU% as delta between two samples, not through the entire lifetime
	var cpuPercent float64
	times, err := h.proc.TimesWithContext(ctx)
	if err != nil {
		return ProcessMetrics{}, fmt.Errorf("cpu times: %w", err)
	}

	now := time.Now()
	currentCPU := times.User + times.System
	elapsed := now.Sub(h.lastSample).Seconds()

	if elapsed > 0 && !h.lastSample.IsZero() {
		deltaCPU := currentCPU - h.lastCPU
		cpuPercent = (deltaCPU / elapsed) * 100
	}

	h.lastCPU = currentCPU
	h.lastSample = now

	memInfo, err := h.proc.MemoryInfoWithContext(ctx)
	if err != nil {
		return ProcessMetrics{}, fmt.Errorf("memory info: %w", err)
	}

	createTime, err := h.proc.CreateTimeWithContext(ctx)
	if err != nil {
		return ProcessMetrics{}, fmt.Errorf("create time: %w", err)
	}

	return ProcessMetrics{
		PID:        h.proc.Pid,
		CPUPercent: cpuPercent,
		RSS:        memInfo.RSS,
		CreateTime: createTime,
		Uptime:     time.Since(time.UnixMilli(createTime)),
	}, nil
}
