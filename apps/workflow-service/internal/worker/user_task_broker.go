package worker

import "sync"

// ParkedUserTask is a user-task job held by the background worker until complete API signals it.
type ParkedUserTask struct {
	JobKey             int64
	JobType            string
	ElementID          string
	ProcessInstanceKey int64
	CaseID             string
	CandidateRole      string
}

type parkedEntry struct {
	task ParkedUserTask
	ch   chan map[string]any
}

// UserTaskBroker parks activated user-task jobs until the HTTP complete API signals them.
type UserTaskBroker struct {
	mu      sync.Mutex
	waiters map[int64]*parkedEntry
}

func NewUserTaskBroker() *UserTaskBroker {
	return &UserTaskBroker{waiters: map[int64]*parkedEntry{}}
}

func (b *UserTaskBroker) Register(task ParkedUserTask) <-chan map[string]any {
	ch := make(chan map[string]any, 1)
	b.mu.Lock()
	b.waiters[task.JobKey] = &parkedEntry{task: task, ch: ch}
	b.mu.Unlock()
	return ch
}

func (b *UserTaskBroker) Remove(jobKey int64) {
	b.mu.Lock()
	delete(b.waiters, jobKey)
	b.mu.Unlock()
}

func (b *UserTaskBroker) FindParked(processInstanceKey int64, jobType string) *ParkedUserTask {
	if processInstanceKey <= 0 {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, entry := range b.waiters {
		task := entry.task
		if task.ProcessInstanceKey != processInstanceKey {
			continue
		}
		if jobType != "" && task.JobType != jobType {
			continue
		}
		copy := task
		return &copy
	}
	return nil
}

func (b *UserTaskBroker) IsParked(jobKey int64) bool {
	b.mu.Lock()
	_, ok := b.waiters[jobKey]
	b.mu.Unlock()
	return ok
}

func (b *UserTaskBroker) SignalComplete(jobKey int64, variables map[string]any) bool {
	b.mu.Lock()
	entry, ok := b.waiters[jobKey]
	b.mu.Unlock()
	if !ok {
		return false
	}
	select {
	case entry.ch <- variables:
		return true
	default:
		return false
	}
}
