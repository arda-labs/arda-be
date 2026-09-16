package service

import "testing"

func TestOrderEODJobsSequenceBeforeCode(t *testing.T) {
	// Same set/order as plt_job_definitions seeds, but shuffled.
	jobs := []eodJob{
		{code: "FIN_TRIAL_BALANCE_DAILY", endpoint: "finance", sequence: 30},
		{code: "LNM_PROVISION_DAILY", endpoint: "loan-provision", sequence: 20},
		{code: "DPM_ACCRUAL_DAILY", endpoint: "deposit", sequence: 15},
		{code: "LNM_ACCRUAL_DAILY", endpoint: "loan-accrual", sequence: 10},
	}
	orderEODJobs(jobs)

	want := []string{
		"LNM_ACCRUAL_DAILY",
		"DPM_ACCRUAL_DAILY",
		"LNM_PROVISION_DAILY",
		"FIN_TRIAL_BALANCE_DAILY",
	}
	for i, code := range want {
		if jobs[i].code != code {
			t.Fatalf("position %d = %s, want %s (got order %v)", i, jobs[i].code, code, codes(jobs))
		}
	}
}

func TestOrderEODJobsCodeTieBreak(t *testing.T) {
	jobs := []eodJob{
		{code: "ZETA", sequence: 10},
		{code: "ALPHA", sequence: 10},
	}
	orderEODJobs(jobs)

	if jobs[0].code != "ALPHA" || jobs[1].code != "ZETA" {
		t.Fatalf("equal sequence must tie-break by code, got %v", codes(jobs))
	}
}

func codes(jobs []eodJob) []string {
	out := make([]string, 0, len(jobs))
	for _, j := range jobs {
		out = append(out, j.code)
	}
	return out
}
