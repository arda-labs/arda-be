# Dev Dependencies

Last updated: 2026-06-27

Local development connects to services running in the k3s LAN cluster.

## Nodes

| Node | IP |
| --- | --- |
| `k3s-node1` | `192.168.100.201` |
| `k3s-node2` | `192.168.100.202` |
| `k3s-node3` | `192.168.100.203` |

## LAN Endpoints

Use any node IP for NodePort services.

| Dependency | Endpoint |
| --- | --- |
| PostgreSQL CNPG | `192.168.100.201:30432` |
| Valkey | `192.168.100.201:30379` |
| Hydra admin | `http://192.168.100.201:30445` |
| Kratos admin | `http://192.168.100.201:30446` |
| Hydra public | `https://auth.arda.io.vn` |
| Kratos public | `https://identity.arda.io.vn` |

NATS is currently ClusterIP only. For local development, use:

```powershell
$env:KUBECONFIG="C:\Users\hoanv\AppData\Roaming\Freelens\kubeconfigs\7f516115-48bd-40a0-b655-4245be8c022a"
kubectl -n platform port-forward svc/nats 4222:4222
```

Then use:

```env
NATS_URL=nats://127.0.0.1:4222
```

## Common Local Env

Copy from `deployments/dev/endpoints.example.env` and fill secrets locally. Do not commit real secrets.

