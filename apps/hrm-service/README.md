# hrm-service

Arda Go microservice.

## Scope

`hrm-service` owns HRM data and HRM business rules.

HRM submits employee registration cases through `workflow-service` gRPC. It does not connect to Zeebe or host Zeebe workers.
