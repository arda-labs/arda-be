# workflow-service

Workflow/BPM service for Arda.

## Scope

`workflow-service` owns the Arda workflow facade. It stores business case metadata, workflow configuration, BPMN process definitions, and process-admin reference data. It integrates with Zeebe 8.5 through the Zeebe Gateway, but product UX is owned by `arda-mfe`.

It must not own finance, customer, or accounting business rules.

## Current API Surface

Runtime compatibility APIs:

```txt
POST /api/workflow/deploy
POST /api/workflow/start
POST /api/workflow/messages
POST /api/workflow/instances/{key}/cancel
GET  /api/workflow/instances/mapping/{businessKey}
```

Case and configuration APIs:

```txt
GET  /api/workflow/case-types
POST /api/workflow/case-types
PUT  /api/workflow/case-types/{caseType}
PUT  /api/workflow/case-types/{caseType}/process-config

GET  /api/workflow/cases
GET  /api/workflow/cases/{id}
GET  /api/workflow/cases/{id}/timeline

GET  /api/workflow/sla-policies
POST /api/workflow/sla-policies
PUT  /api/workflow/sla-policies/{id}

GET  /api/workflow/description-templates
POST /api/workflow/description-templates
PUT  /api/workflow/description-templates/{id}

GET  /api/workflow/process-definitions
POST /api/workflow/process-definitions
PUT  /api/workflow/process-definitions/{id}
GET  /api/workflow/process-definitions/{id}/xml
POST /api/workflow/process-definitions/{id}/deploy

GET  /api/workflow/roles
POST /api/workflow/roles
PUT  /api/workflow/roles/{id}

GET  /api/workflow/role-catalog
POST /api/workflow/role-catalog
PUT  /api/workflow/role-catalog/{roleCode}

GET  /api/workflow/role-memberships
POST /api/workflow/role-memberships
PUT  /api/workflow/role-memberships/{id}

GET  /api/workflow/assignment-rules
POST /api/workflow/assignment-rules
PUT  /api/workflow/assignment-rules/{id}

GET  /api/workflow/delegations
POST /api/workflow/delegations
PUT  /api/workflow/delegations/{id}
```

Legacy/versioned aliases also exist for deploy/start/message and instance operations under `/api/v1/workflows/*`.

## Persistence Areas

Migrations currently create or extend:

- `business_cases`
- `business_operation_types`
- `business_sla_policies`
- `business_sla_task_policies`
- `business_description_templates`
- `business_process_roles`
- `workflow_role_catalog`
- `workflow_role_memberships`
- `workflow_assignment_rules`
- `workflow_delegations`
- `workflow_process_definitions`

## Run And Test

From this service directory:

```bash
go test ./...
```

From the backend workspace root, run service-specific tests instead of `go test ./...`; the root uses `go.work` and is not itself a Go module.

## Known Gaps

- No runtime task claim/complete/reassign facade yet.
- No incident retry/suspend/resume APIs yet.
- Role membership and assignment rules are persisted but not yet used to resolve task candidates.
- Process definitions can be deployed, but monitor panels still need real incident/job/called-instance data from Zeebe.
