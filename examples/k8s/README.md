# Kubernetes Deployment Examples

This directory contains example Kubernetes manifests for deploying hive-server with CockroachDB.

## Files

- `secret.yaml` - Kubernetes Secret containing DATABASE_URL and HIVE_TOKEN
- `deployment.yaml` - Deployment and Service manifests for hive-server

## Quick Start

1. **Update the secret** with your actual CockroachDB connection string:
   ```bash
   # Base64 encode your DATABASE_URL
   echo -n "postgresql://hive_user:password@cockroachdb:26257/hive?sslmode=require" | base64
   
   # Update secret.yaml with the encoded value
   ```

2. **Apply the manifests**:
   ```bash
   kubectl apply -f secret.yaml
   kubectl apply -f deployment.yaml
   ```

## Configuration

### Database Connection

The deployment expects a `DATABASE_URL` environment variable pointing to a CockroachDB or PostgreSQL database:

```
postgresql://username:password@host:port/database?sslmode=require
```

### Authentication (Optional)

Set `HIVE_TOKEN` in the secret for API authentication. If not set, authentication is disabled (suitable for internal/development use).

## Health Checks

The deployment uses `/healthz` for both liveness and readiness probes. This endpoint:

- Returns `200 OK` with `{"status": "healthy"}` when the database is accessible
- Returns `503 Service Unavailable` with error details when the database is unreachable

## Notes

- The deployment runs without SQLite PVC volumes (CockroachDB replaces local storage)
- Uses security best practices (non-root user, read-only filesystem, minimal capabilities)
- Includes resource requests/limits for proper cluster resource management