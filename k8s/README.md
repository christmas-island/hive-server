# hive-server Kubernetes Deployment

Manifests for deploying hive-server to a Kubernetes cluster.

## Deploy

```bash
kubectl apply -k k8s/
```

## Verify

```bash
kubectl get pods -l app=app
kubectl logs -l app=app
curl http://<service-ip>:8080/health
```

## Configuration

Set environment variables in the deployment:

| Variable       | Description               | Default         |
| -------------- | ------------------------- | --------------- |
| `PORT`         | Listen port               | `8080`          |
| `HIVE_TOKEN`   | Bearer token for API auth | _(none)_        |
| `HIVE_DB_PATH` | SQLite database path      | `/data/hive.db` |

SQLite data lives on a PersistentVolumeClaim mounted at `/data`.

## Files

| File                  | Purpose                           |
| --------------------- | --------------------------------- |
| `deployment.yaml`     | Pod spec, env, volume mounts      |
| `service.yaml`        | ClusterIP / LoadBalancer services |
| `ingress.yaml`        | External access with TLS          |
| `hpa.yaml`            | Horizontal Pod Autoscaler         |
| `pdb.yaml`            | Pod Disruption Budget             |
| `serviceaccount.yaml` | RBAC                              |
| `kustomization.yaml`  | Kustomize base                    |

## Notes

- SQLite requires `ReadWriteOnce` PVC — single replica only (or use `Recreate` strategy)
- Health probes use `/health` on port 8080
