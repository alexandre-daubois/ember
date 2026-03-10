package fetcher

import (
	"context"
	"fmt"
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

func fetchProcessMetrics(ctx context.Context, pid int32) (ProcessMetrics, error) {
	if pid <= 0 {
		return ProcessMetrics{}, nil
	}

	p, err := process.NewProcess(pid)
	if err != nil {
		return ProcessMetrics{}, fmt.Errorf("process %d: %w", pid, err)
	}

	cpu, err := p.CPUPercentWithContext(ctx)
	if err != nil {
		return ProcessMetrics{}, fmt.Errorf("cpu percent: %w", err)
	}

	memInfo, err := p.MemoryInfoWithContext(ctx)
	if err != nil {
		return ProcessMetrics{}, fmt.Errorf("memory info: %w", err)
	}

	createTime, err := p.CreateTimeWithContext(ctx)
	if err != nil {
		return ProcessMetrics{}, fmt.Errorf("create time: %w", err)
	}

	return ProcessMetrics{
		PID:        pid,
		CPUPercent: cpu,
		RSS:        memInfo.RSS,
		CreateTime: createTime,
		Uptime:     time.Since(time.UnixMilli(createTime)),
	}, nil
}
