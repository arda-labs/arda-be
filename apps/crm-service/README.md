# crm-service

Arda Go microservice.

## Scope

`crm-service` owns customer data and customer business rules.

CRM submits workflow cases through `workflow-service` gRPC. It does not connect to Zeebe or host Zeebe workers.
