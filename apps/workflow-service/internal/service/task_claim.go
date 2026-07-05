package service

type TaskClaimFilter struct {
	ProcessInstanceKey int64
	CaseID             string
	ElementID          string
}

func matchesTaskClaimFilter(task WorkflowTask, filter TaskClaimFilter) bool {
	if filter.ProcessInstanceKey > 0 && task.ProcessInstanceKey != filter.ProcessInstanceKey {
		return false
	}
	if filter.CaseID != "" && task.CaseID != "" && task.CaseID != filter.CaseID {
		return false
	}
	if filter.ElementID != "" && task.ElementID != "" && task.ElementID != filter.ElementID {
		return false
	}
	if filter.ProcessInstanceKey == 0 && filter.CaseID == "" && filter.ElementID == "" {
		return true
	}
	return filter.ProcessInstanceKey > 0 || filter.CaseID != "" || filter.ElementID != ""
}
