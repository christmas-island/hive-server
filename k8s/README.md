# Finagle Kubernetes Deployment

This directory contains Kubernetes manifests for deploying Finagle v0.22.0 to a
DigitalOcean Kubernetes cluster.

## Overview

The deployment includes:

- **Deployment**: Main application deployment with 3 replicas
- **Services**: LoadBalancer and ClusterIP services for HTTP (8080) and
  TCP (9999) ports
- **ConfigMap & Secrets**: Application configuration and sensitive data
- **HPA**: Horizontal Pod Autoscaler for automatic scaling
- **PDB**: Pod Disruption Budget for high availability
- **Ingress**: External access with SSL termination
- **ServiceAccount & RBAC**: Security and permissions
- **NetworkPolicy**: Network security policies
- **Monitoring**: ServiceMonitor for Prometheus metrics

## Prerequisites

1. **DigitalOcean Kubernetes Cluster**: Ensure you have a DOKS cluster running
2. **kubectl**: Configured to access your cluster
3. **Cert-Manager**: For SSL certificate management (optional)
4. **NGINX Ingress Controller**: For ingress functionality (optional)
5. **Prometheus Operator**: For monitoring (optional)

## Quick Start

### 1. Deploy All Resources

```bash
# Apply all manifests
kubectl apply -f k8s/

# Or use kustomize
kubectl apply -k k8s/
```

### 2. Verify Deployment

```bash
# Check deployment status
kubectl get deployments

# Check pods
kubectl get pods -l app=app

# Check services
kubectl get services -l app=app

# Check ingress
kubectl get ingress
```

### 3. Access the Application

```bash
# Get the external IP
kubectl get service app-service

# Test HTTP endpoint
curl http://<EXTERNAL-IP>/ping

# Test version endpoint
curl http://<EXTERNAL-IP>/version
```

## Configuration

### Environment Variables

The application supports the following environment variables:

- `FINAGLE_DEBUG`: Enable debug logging (default: false)
- `FINAGLE_STORAGE`: Storage directory path (default: /data/storage)
- `FINAGLE_LINE_COUNT`: Initial line count (default: 1000)

### Resource Limits

Default resource configuration:

- **Requests**: 256Mi memory, 100m CPU
- **Limits**: 512Mi memory, 500m CPU

### Scaling

- **Min Replicas**: 3
- **Max Replicas**: 10
- **CPU Target**: 70%
- **Memory Target**: 80%

## Security Features

### Security Context

- Non-root user (UID 1000)
- Read-only root filesystem
- Dropped all capabilities
- No privilege escalation

### Network Security

- Network policies restricting ingress/egress
- Service account with minimal permissions
- RBAC for pod access

### Pod Security

- Pod anti-affinity for high availability
- Pod disruption budget (min 2 available)
- Graceful shutdown handling

## Monitoring

### Health Checks

- **Liveness Probe**: `/ping` endpoint every 10s
- **Readiness Probe**: `/ping` endpoint every 5s
- **Startup Probe**: `/ping` endpoint with 12 failure threshold

### Metrics

- Prometheus ServiceMonitor configured
- Metrics endpoint: `/metrics` on port 8080
- Scrape interval: 30s

## Customization

### Domain Configuration

Update the ingress manifests with your domain:

```yaml
# In ingress.yaml
spec:
  tls:
    - hosts:
        - your-domain.com # Change this
```

### Load Balancer Configuration

For DigitalOcean LoadBalancer, update the service annotations:

```yaml
# In service.yaml
metadata:
  annotations:
    service.beta.kubernetes.io/do-loadbalancer-name: "your-lb-name"
```

### Resource Adjustments

Modify resource requests/limits in `deployment.yaml`:

```yaml
resources:
  requests:
    memory: "512Mi" # Adjust as needed
    cpu: "200m"
  limits:
    memory: "1Gi"
    cpu: "1000m"
```

## Troubleshooting

### Common Issues

1. **Pods not starting**: Check resource limits and node capacity
2. **Service not accessible**: Verify ingress controller and DNS
3. **Health checks failing**: Check application logs and endpoint availability

### Debugging Commands

```bash
# Check pod logs
kubectl logs -l app=app

# Describe deployment
kubectl describe deployment app

# Check events
kubectl get events --sort-by=.metadata.creationTimestamp

# Port forward for local testing
kubectl port-forward service/app-internal 8080:8080
```

## Production Considerations

### High Availability

- Deploy across multiple availability zones
- Use persistent volumes for data storage
- Configure backup strategies

### Performance

- Monitor resource usage and adjust limits
- Consider node affinity for optimal placement
- Implement proper logging and monitoring

### Security

- Regularly update base images
- Use image scanning tools
- Implement network segmentation
- Monitor for security vulnerabilities

## Support

For issues related to:

- **Application**: Check Finagle documentation
- **Kubernetes**: Refer to Kubernetes documentation
- **DigitalOcean**: Check DOKS documentation
