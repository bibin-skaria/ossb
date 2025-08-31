# Kaniko vs OSSB - Container Builder Comparison

This guide shows how to migrate from Kaniko to OSSB for rootless container builds with enhanced multi-architecture support.

## 🏗️ Architecture Comparison

| Feature | Kaniko | OSSB |
|---------|--------|------|
| **Runtime** | Go binary + userspace | Go binary + Rootless Podman |
| **Multi-arch** | Limited, experimental | Native support with QEMU |
| **Caching** | Layer-based | Content-addressable (SHA256) |
| **Base Image** | scratch + dependencies | Alpine + container runtime |
| **Rootless** | ✅ Yes | ✅ Yes + User namespaces |
| **Registry Push** | Direct | Via rootless runtime |
| **Output Formats** | Image only | Image, Tar, Local, Multi-arch |

## 🔄 Command Migration Guide

### Basic Build Command

**Kaniko:**
```bash
docker run --rm -v $(pwd):/workspace \
  gcr.io/kaniko-project/executor:latest \
  --context=/workspace \
  --dockerfile=Dockerfile \
  --destination=myregistry/myapp:latest
```

**OSSB Equivalent:**
```bash
docker run --rm -v $(pwd):/workspace \
  ossb:latest \
  --context=/workspace \
  --dockerfile=Dockerfile \
  --destination=myregistry/myapp:latest
```

### Multi-Architecture Build

**Kaniko (Limited):**
```bash
# Kaniko requires separate builds per architecture
docker run --rm -v $(pwd):/workspace \
  gcr.io/kaniko-project/executor:latest \
  --context=/workspace \
  --dockerfile=Dockerfile \
  --destination=myregistry/myapp:latest-amd64 \
  --custom-platform=linux/amd64
```

**OSSB (Native Multi-Arch):**
```bash
docker run --rm -v $(pwd):/workspace \
  ossb:latest \
  --context=/workspace \
  --dockerfile=Dockerfile \
  --destination=myregistry/myapp:latest \
  --custom-platform=linux/amd64,linux/arm64,linux/arm/v7
```

### With Caching

**Kaniko:**
```bash
docker run --rm -v $(pwd):/workspace \
  gcr.io/kaniko-project/executor:latest \
  --context=/workspace \
  --dockerfile=Dockerfile \
  --destination=myregistry/myapp:latest \
  --cache=true \
  --cache-repo=myregistry/cache
```

**OSSB:**
```bash
docker run --rm -v $(pwd):/workspace \
  ossb:latest \
  --context=/workspace \
  --dockerfile=Dockerfile \
  --destination=myregistry/myapp:latest \
  --cache=true
```

## 🚀 Kubernetes Deployment Examples

### Kaniko Job

```yaml
apiVersion: batch/v1
kind: Job
metadata:
  name: kaniko-build
spec:
  template:
    spec:
      containers:
      - name: kaniko
        image: gcr.io/kaniko-project/executor:latest
        args:
        - "--context=git://github.com/user/repo"
        - "--destination=myregistry/myapp:latest"
        volumeMounts:
        - name: docker-config
          mountPath: /kaniko/.docker
      volumes:
      - name: docker-config
        secret:
          secretName: regcred
      restartPolicy: Never
```

### OSSB Job (Direct Migration)

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
        args:
        - "--context=git://github.com/user/repo"
        - "--destination=myregistry/myapp:latest"
        - "--custom-platform=linux/amd64,linux/arm64"  # Multi-arch!
        volumeMounts:
        - name: docker-config
          mountPath: /home/ossb/.docker
        securityContext:
          runAsUser: 9999
          runAsGroup: 9999
      volumes:
      - name: docker-config
        secret:
          secretName: regcred
      restartPolicy: Never
```

### OSSB Job with Enhanced Features

```yaml
apiVersion: batch/v1
kind: Job
metadata:
  name: ossb-multiarch-build
spec:
  template:
    spec:
      containers:
      - name: ossb
        image: ossb:latest
        args:
        - "--context=/workspace"
        - "--dockerfile=Dockerfile"
        - "--destination=myregistry/myapp:latest"
        - "--custom-platform=linux/amd64,linux/arm64,linux/arm/v7"
        - "--force"  # Push to registry
        - "--cache=true"
        resources:
          requests:
            memory: "1Gi"
            cpu: "500m"
          limits:
            memory: "4Gi" 
            cpu: "2"
        volumeMounts:
        - name: workspace
          mountPath: /workspace
        - name: docker-config
          mountPath: /home/ossb/.docker
        - name: cache-volume
          mountPath: /home/ossb/.ossb/cache
        securityContext:
          runAsUser: 9999
          runAsGroup: 9999
          allowPrivilegeEscalation: false
          capabilities:
            add: ["SYS_ADMIN"]  # For user namespaces
      volumes:
      - name: workspace
        persistentVolumeClaim:
          claimName: build-workspace
      - name: docker-config
        secret:
          secretName: regcred
      - name: cache-volume
        persistentVolumeClaim:
          claimName: ossb-cache
      restartPolicy: Never
```

## 🔧 GitLab CI/CD Migration

### From Kaniko

```yaml
build:
  stage: build
  image:
    name: gcr.io/kaniko-project/executor:debug
    entrypoint: [""]
  script:
    - mkdir -p /kaniko/.docker
    - echo "{\"auths\":{\"$CI_REGISTRY\":{\"username\":\"$CI_REGISTRY_USER\",\"password\":\"$CI_REGISTRY_PASSWORD\"}}}" > /kaniko/.docker/config.json
    - /kaniko/executor
      --context $CI_PROJECT_DIR
      --dockerfile $CI_PROJECT_DIR/Dockerfile
      --destination $CI_REGISTRY_IMAGE:$CI_COMMIT_TAG
```

### To OSSB

```yaml
build:
  stage: build
  image:
    name: ossb:latest
    entrypoint: [""]
  script:
    - mkdir -p /home/ossb/.docker
    - echo "{\"auths\":{\"$CI_REGISTRY\":{\"username\":\"$CI_REGISTRY_USER\",\"password\":\"$CI_REGISTRY_PASSWORD\"}}}" > /home/ossb/.docker/config.json
    - /usr/local/bin/ossb-executor
      --context $CI_PROJECT_DIR
      --dockerfile $CI_PROJECT_DIR/Dockerfile
      --destination $CI_REGISTRY_IMAGE:$CI_COMMIT_TAG
      --custom-platform linux/amd64,linux/arm64
      --force
```

## 🎯 Direct OSSB Usage (Without Kaniko Wrapper)

For more control, use OSSB directly:

```yaml
build-multiarch:
  stage: build
  image: ossb:latest
  script:
    - echo "{\"auths\":{\"$CI_REGISTRY\":{\"username\":\"$CI_REGISTRY_USER\",\"password\":\"$CI_REGISTRY_PASSWORD\"}}}" > /home/ossb/.docker/config.json
    - ossb build . 
      -t $CI_REGISTRY_IMAGE:$CI_COMMIT_TAG 
      --platform linux/amd64,linux/arm64,linux/arm/v7
      --rootless 
      --push 
      --registry $CI_REGISTRY
```

## 📊 Performance Comparison

| Metric | Kaniko | OSSB |
|--------|--------|------|
| **Single arch build** | ~2-3 min | ~2-3 min |
| **Multi-arch build** | ~6-9 min (3 separate builds) | ~4-5 min (parallel) |
| **Cache efficiency** | Layer-based (good) | Content-addressable (excellent) |
| **Memory usage** | ~500MB-1GB | ~1-2GB (includes runtime) |
| **Container size** | ~50MB | ~150MB |
| **Startup time** | ~5s | ~10s |

## 🔐 Security Comparison

| Security Feature | Kaniko | OSSB |
|------------------|--------|------|
| **Rootless execution** | ✅ | ✅ |
| **User namespaces** | ❌ | ✅ |
| **No Docker daemon** | ✅ | ✅ (uses rootless runtime) |
| **Minimal attack surface** | ✅ (smaller) | ⚠️ (larger but isolated) |
| **Secret handling** | File-based | Runtime + File-based |

## 🚀 Migration Checklist

### Phase 1: Direct Replacement
- [ ] Replace `gcr.io/kaniko-project/executor` with `ossb:latest`
- [ ] Update volume mount paths (`/kaniko/.docker` → `/home/ossb/.docker`)
- [ ] Test basic single-architecture builds
- [ ] Verify registry authentication works

### Phase 2: Enable Multi-Architecture
- [ ] Add `--custom-platform=linux/amd64,linux/arm64` to builds
- [ ] Test multi-arch manifest list creation
- [ ] Update resource limits (CPU/memory) for multi-arch builds
- [ ] Verify QEMU emulation works in your environment

### Phase 3: Optimize Performance
- [ ] Implement persistent cache volumes
- [ ] Tune resource requests/limits
- [ ] Enable parallel builds where possible
- [ ] Monitor cache hit rates

### Phase 4: Advanced Features
- [ ] Switch to native OSSB commands (drop Kaniko compatibility layer)
- [ ] Implement advanced caching strategies
- [ ] Use OSSB-specific output formats (tar, local)
- [ ] Integrate with OSSB cache management

## ⚡ Quick Migration Commands

### Build the OSSB image:
```bash
docker build -f Dockerfile.ossb -t ossb:latest .
```

### Test compatibility:
```bash
# Test basic build (Kaniko-style)
docker run --rm -v $(pwd):/workspace ossb:latest \
  --context=/workspace \
  --dockerfile=Dockerfile \
  --destination=test:latest

# Test multi-arch build (OSSB advantage)
docker run --rm -v $(pwd):/workspace ossb:latest \
  --context=/workspace \
  --dockerfile=Dockerfile \
  --destination=test:latest \
  --custom-platform=linux/amd64,linux/arm64
```

## 🎉 OSSB Advantages Over Kaniko

1. **🏗️ Native Multi-Architecture**: Single command builds for multiple platforms
2. **📦 Content-Addressable Cache**: More efficient caching than layer-based
3. **🔒 User Namespace Isolation**: Enhanced security with proper user namespaces  
4. **📁 Multiple Output Formats**: Not just images - tar, local filesystem too
5. **🚀 Better Performance**: Parallel multi-arch builds vs sequential
6. **🛠️ More Control**: Direct access to build process and caching
7. **📈 Scalability**: Better resource utilization for multi-platform CI/CD

OSSB provides a **drop-in replacement** for Kaniko with **enhanced multi-architecture capabilities** and **better performance** for modern container build workflows! 🚀