package bootstrap

import (
	"regexp"
	"slices"
	"testing"

	"github.com/arda-labs/arda/apps/workflow-service/internal/worker"
	"github.com/arda-labs/arda/libs/go/arda-grpc/client/loan"
)

// Every serviceTask in the embedded BPMN files must map to a job topic that
// main.go actually registers a worker for. A mismatch is not a cosmetic
// problem: the process instance parks on that task forever (unresolved job /
// incident) and the business case never reaches COMPLETED — exactly the bug
// that silently broke customer-adjustment-v2 APPROVE.
//
// intentionallyUncovered documents sample/reference processes that are
// deployed but must not be started by case types until their domain workers
// exist; each entry must carry a matching comment in processes.go.
var intentionallyUncovered = map[string][]string{
	"LOAN_FORMATION_V2": {
		// "Reference flow only, no domain worker yet" (processes.go)
		"lnm.loan.formation.validate",
		"lnm.loan.formation.execute",
		"lnm.loan.formation.cancel",
	},
}

func TestEveryServiceTaskTopicHasRegisteredWorker(t *testing.T) {
	topicPattern := regexp.MustCompile(`zeebe:taskDefinition[^>]*type="([^"]+)"`)

	registered := make([]string, 0, len(worker.RegisteredJobTopics)+len(loan.Kinds)*3)
	registered = append(registered, worker.RegisteredJobTopics...)
	registered = append(registered, worker.RegisteredLNMTopics(loan.Kinds)...)

	for _, process := range BuiltInProcesses() {
		matches := topicPattern.FindAllStringSubmatch(string(process.Content), -1)
		if len(matches) == 0 {
			t.Errorf("process %s (%s): no zeebe:taskDefinition found — file malformed or pattern stale", process.ProcessCode, process.ResourceName)
			continue
		}
		for _, match := range matches {
			topic := match[1]
			if slices.Contains(registered, topic) {
				continue
			}
			if slices.Contains(intentionallyUncovered[process.ProcessCode], topic) {
				continue
			}
			t.Errorf("process %s: serviceTask topic %q has no registered worker in cmd/workflow-service/main.go", process.ProcessCode, topic)
		}
	}
}
