package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"gkx/wcplus/internal/wcplus"
)

type WaitQueueRequest struct {
	TimeoutSec int `json:"timeoutSec"`
	IntervalSec int `json:"intervalSec"`
}

type WaitQueueResult struct {
	Done        bool                `json:"done"`
	WaitedSec   int                 `json:"waitedSec"`
	LastTasks   wcplus.TaskAllResponse `json:"lastTasks"`
}

func (s *SyncService) WaitQueue(ctx context.Context, req WaitQueueRequest) (WaitQueueResult, error) {
	timeout := req.TimeoutSec
	if timeout <= 0 {
		timeout = 600
	}
	interval := req.IntervalSec
	if interval <= 0 {
		interval = 10
	}
	start := time.Now()
	deadline := start.Add(time.Duration(timeout) * time.Second)
	var last wcplus.TaskAllResponse
	for {
		tasks, err := s.wc.AllTasks(ctx)
		if err != nil {
			return WaitQueueResult{}, err
		}
		last = tasks
		if queueIdle(tasks) {
			return WaitQueueResult{
				Done:      true,
				WaitedSec: int(time.Since(start).Seconds()),
				LastTasks: last,
			}, nil
		}
		if time.Now().After(deadline) {
			return WaitQueueResult{
				Done:      false,
				WaitedSec: int(time.Since(start).Seconds()),
				LastTasks: last,
			}, fmt.Errorf("wait queue timeout after %ds", timeout)
		}
		select {
		case <-ctx.Done():
			return WaitQueueResult{Done: false, LastTasks: last}, ctx.Err()
		case <-time.After(time.Duration(interval) * time.Second):
		}
	}
}

func queueIdle(tasks wcplus.TaskAllResponse) bool {
	if len(tasks.Tasks) == 0 {
		return true
	}
	for _, t := range tasks.Tasks {
		st := strings.ToLower(strings.TrimSpace(t.Status))
		if st == "running" || st == "ready" || st == "pending" {
			return false
		}
	}
	return true
}
