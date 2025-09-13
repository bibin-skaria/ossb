# Kubernetes Integration

This package provides comprehensive Kubernetes integration for OSSB (Open Source Slim Builder), enabling seamless container image builds within Kubernetes pods without requiring privileged access or Docker daemon.

## Features

### 🔐 Secret Management
- **Registry Credentials**: Automatic discovery from Docker config secrets and individual credential secrets
- **Build Secrets**: Support for build-time secrets mounted as Kubernetes secrets
- **Multiple Auth Methods**: Docker Hub, private registries, and cloud registries (ECR, GCR, ACR)

### 📊 Structured Logging
- **JSON Format**: Kubernetes-compatible structured logging with proper field mapping
- **Log Levels**: Configurable log levels via environment variables
- **Context Tracking**: Build ID, job name, pod name, and namespace tracking
- **Event Types**: Specialized logging for build events, registry operations, and progress

### 🔄 Job Lifecycle Management
- **Signal Handling**: Graceful shutdown on SIGTERM/SIGINT
- **Progress Reporting**: Real-time build progress with stage-level granularity
- **Exit Codes**: Proper exit codes for different failure scenarios
- **Cleanup**: Automatic resource cleanup on job completion

### 🏗️ Build Context Management
- **ConfigMap Support**: Build context from Kubernetes ConfigMaps
- **Volume Mounting**: Support for build context from mounted volumes
- **Workspace Setup**: Automatic workspace directory creation and management

## Usage

### Basic Kubernetes Job

```yaml
apiVersion: batch/v1
kind: Job
metadata:
  name: ossb-build
spec:
  template:
    spec:
      containers:
      - name: ossb
        image: ossb:latest
        command: ["ossb", "build", "--push", "--tag=myapp:latest", "/workspace/context"]
        env:
        - name: JOB_NAME
          valueFrom:
            fieldRef:
              fieldPath: metadata.labels['job-name']
        - name: POD_NAME
          valueFrom:
            fieldRef:
              fieldPath: metadata.name
        - name: POD_NAMESPACE
          valueFrom:
            fieldRef:
              fieldPath: metadata.namespace
        volumeMounts:
        - name: build-context
          mountPath: /workspace/context
        - name: registry-secret
          mountPath: /var/run/secrets/registry-secret
      volumes:
      - name: build-context
        configMap:
          name: build-context
      - name: registry-secret
        secret:
          secretName: registry-credentials
```

### Registry Credentials

#### Docker Config Secret
```yaml
apiVersion: v1
kind: Secret
metadata:
  name: registry-credentials
type: kubernetes.io/dockerconfigjson
data:
  .dockerconfigjson: <base64-encoded-docker-config>
```

#### Individual Registry Secret
```yaml
apiVersion: v1
kind: Secret
metadata:
  name: registry-auth
type: Opaque
data:
  registry: <base64-encoded-registry-url>
  username: <base64-encoded-username>
  password: <base64-encoded-password>
```

### Build Context ConfigMap

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: build-context
data:
  Dockerfile: |
    FROM alpine:latest
    RUN apk add --no-cache curl
    COPY app.sh /app.sh
    CMD ["/app.sh"]
  app.sh: |
    #!/bin/sh
    echo "Hello from OSSB!"
```

### Registry Configuration

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: registry-config
data:
  config.yaml: |
    default_registry: docker.io
    registries:
      gcr.io:
        username: _json_key
        auth_file: /var/run/secrets/gcr-key/key.json
    insecure:
      - localhost:5000
    mirrors:
      docker.io:
        - mirror.gcr.io
```

## Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `JOB_NAME` | Kubernetes job name | - |
| `POD_NAME` | Kubernetes pod name | - |
| `POD_NAMESPACE` | Kubernetes namespace | - |
| `NODE_NAME` | Kubernetes node name | - |
| `LOG_LEVEL` | Log level (debug, info, warn, error) | `info` |

## Secret Paths

| Path | Description |
|------|-------------|
| `/var/run/secrets/registry-secret/.dockerconfigjson` | Docker config JSON |
| `/var/run/secrets/registry-auth/` | Individual registry credentials |
| `/var/run/secrets/build-secrets/` | Build-time secrets |
| `/var/run/configmaps/registry-config/config.yaml` | Registry configuration |
| `/var/run/configmaps/build-context/` | Build context files |

## Exit Codes

| Code | Description |
|------|-------------|
| 0 | Success |
| 1 | General error |
| 2 | Build error |
| 3 | Registry error |
| 4 | Authentication error |
| 5 | Configuration error |
| 6 | Resource error |
| 7 | Timeout error |
| 8 | Cancelled error |

## Monitoring

### Progress Reporting
OSSB writes progress information to `/tmp/ossb-progress.json`:

```json
{
  "stage": "build",
  "progress": 75.0,
  "message": "Building layer 3",
  "timestamp": "2025-09-13T12:00:00Z",
  "platform": "linux/amd64",
  "operation": "RUN",
  "cache_hit": false
}
```

### Job Status
Job status is written to `/tmp/ossb-status.json`:

```json
{
  "status": "Succeeded",
  "message": "Build completed successfully",
  "timestamp": "2025-09-13T12:05:00Z",
  "progress": 100.0,
  "build_result": {
    "success": true,
    "operations": 15,
    "cache_hits": 8,
    "duration": "45s"
  }
}
```

## Security

### Rootless Operation
OSSB runs without root privileges by default:

```yaml
securityContext:
  runAsNonRoot: true
  runAsUser: 1000
  runAsGroup: 1000
  allowPrivilegeEscalation: false
  capabilities:
    drop:
    - ALL
```

### Resource Limits
Recommended resource limits:

```yaml
resources:
  requests:
    memory: "1Gi"
    cpu: "500m"
    ephemeral-storage: "10Gi"
  limits:
    memory: "4Gi"
    cpu: "2"
    ephemeral-storage: "20Gi"
```

## Multi-Architecture Builds

OSSB supports multi-architecture builds in Kubernetes:

```bash
ossb build --platform=linux/amd64,linux/arm64 --push --tag=myapp:latest /workspace/context
```

The job will:
1. Build images for each specified platform
2. Create a manifest list
3. Push both individual images and the manifest list
4. Report progress for each platform separately

## Troubleshooting

### Common Issues

1. **Build Context Not Found**
   - Ensure ConfigMap or volume is properly mounted
   - Check `/var/run/configmaps/build-context/` exists

2. **Registry Authentication Failed**
   - Verify secret is mounted at `/var/run/secrets/registry-secret/`
   - Check secret format matches expected structure

3. **Permission Denied**
   - Ensure proper SecurityContext is set
   - Verify service account has necessary permissions

4. **Resource Limits**
   - Check pod resource limits are sufficient
   - Monitor `/tmp/ossb-progress.json` for resource usage

### Debug Logging

Enable debug logging:

```yaml
env:
- name: LOG_LEVEL
  value: "debug"
```

### Health Checks

Monitor job status:

```bash
kubectl logs <pod-name> | jq 'select(.component == "job_status")'
```

Check progress:

```bash
kubectl exec <pod-name> -- cat /tmp/ossb-progress.json
```

## Examples

See the `examples/` directory for complete Kubernetes manifests including:
- Basic build job
- Multi-architecture build
- Private registry authentication
- Build secrets usage
- Resource limits and security contexts

## API Reference

### KubernetesIntegration

Main integration class providing Kubernetes-specific functionality.

#### Methods

- `IsRunningInKubernetes() bool` - Detect Kubernetes environment
- `LoadRegistryCredentials() (*RegistryConfig, error)` - Load registry auth
- `LoadBuildSecrets() (map[string]string, error)` - Load build secrets
- `SetupWorkspace(size string) error` - Setup workspace directories
- `MountBuildContext(path string) error` - Mount build context
- `ReportProgress(...)` - Report build progress
- `SetJobStatus(...)` - Set job status

### JobLifecycleManager

Manages the complete lifecycle of OSSB jobs in Kubernetes.

#### Methods

- `Start(ctx) (context.Context, error)` - Initialize job lifecycle
- `Complete(ctx, result) ExitCode` - Complete successful build
- `Fail(ctx, error, stage) ExitCode` - Handle build failure
- `Cancel(ctx, reason) ExitCode` - Cancel build
- `ReportProgress(...)` - Report progress with logging
- `AddCleanupFunc(func() error)` - Add cleanup function

### StructuredLogger

Kubernetes-compatible structured logging.

#### Methods

- `LogBuildStart(ctx, platforms, tags)` - Log build start
- `LogBuildComplete(ctx, success, duration, ops, hits)` - Log completion
- `LogStageStart/Complete(...)` - Log stage lifecycle
- `LogOperation(...)` - Log individual operations
- `LogRegistryOperation(...)` - Log registry operations
- `LogError(...)` - Log errors with context