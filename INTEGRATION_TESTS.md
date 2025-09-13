# OSSB Integration Test Suite

This document describes the comprehensive integration test suite for OSSB (Open Source Slim Builder), covering end-to-end testing scenarios, registry integration, Kubernetes deployment testing, and performance benchmarking.

## Overview

The integration test suite validates OSSB's functionality across multiple dimensions:

1. **End-to-End Build Testing** - Complete build workflows with various Dockerfile patterns
2. **Registry Integration** - Image pulling, pushing, and authentication with different registries
3. **Kubernetes Integration** - Pod execution, secret handling, and job lifecycle management
4. **Performance Benchmarking** - Comparison with Kaniko and BuildKit across different scenarios
5. **Multi-Architecture Support** - Cross-platform builds with emulation
6. **Security Testing** - Rootless execution and privilege isolation

## Test Structure

### Core Integration Tests (`integration_test.go`)

#### Single Architecture Builds
- **Simple Alpine Build**: Basic FROM, RUN, COPY, CMD instructions
- **Ubuntu with Build Args**: Package installation with build arguments
- **Node.js Application**: Multi-step build with npm dependencies
- **Complex Dockerfile Patterns**: COPY patterns, ENV/LABEL usage, USER/WORKDIR

#### Multi-Architecture Builds
- **Cross-Platform Alpine**: Building for amd64, arm64, arm/v7
- **Architecture Detection**: Verifying correct architecture in built images
- **Manifest List Generation**: Multi-arch manifest creation and validation

#### Multi-Stage Builds
- **Simple Multi-Stage**: Basic builder and runtime stages
- **Complex Dependencies**: Multiple stages with cross-stage copying
- **Target-Specific Builds**: Building specific intermediate stages

#### Dockerfile Pattern Testing
- **COPY/ADD Patterns**: File copying, directory handling, archive extraction
- **Environment Variables**: ENV setting and variable expansion
- **User and Permissions**: USER directive, file ownership, permission handling
- **Volumes and Networking**: VOLUME, EXPOSE, HEALTHCHECK instructions

### Registry Integration Tests (`registry_integration_test.go`)

#### Base Image Pulling
- **Public Registry Access**: Docker Hub, Alpine, Ubuntu, Node.js images
- **Private Registry Access**: Authentication with private registries
- **Registry Caching**: Cache behavior for repeated pulls
- **Error Handling**: Invalid images, network timeouts, authentication failures

#### Image Pushing
- **Single Architecture Push**: Pushing built images to registries
- **Multi-Architecture Push**: Manifest list and platform-specific pushes
- **Authentication Methods**: Username/password, tokens, service accounts
- **Registry Compatibility**: Docker Hub, GCR, ECR, private registries

#### Authentication Testing
- **Anonymous Access**: Public registry access without credentials
- **Credential Discovery**: Docker config, environment variables, Kubernetes secrets
- **Service Account Keys**: GCP service account authentication
- **Token-Based Auth**: Registry tokens and OAuth flows

### Kubernetes Integration Tests (`kubernetes_integration_test.go`)

#### Job Execution
- **Pod Lifecycle**: Job start, progress reporting, completion
- **Build Context Mounting**: ConfigMap and volume-based context mounting
- **Resource Management**: Memory and CPU limit enforcement
- **Exit Code Handling**: Success, failure, and cancellation scenarios

#### Secret Management
- **Registry Credentials**: Docker config secrets, individual credential secrets
- **Build Secrets**: Application secrets, SSL certificates, API keys
- **Secret Discovery**: Automatic credential loading from mounted secrets
- **Security Context**: Rootless execution, user namespace handling

#### Progress Reporting
- **Build Progress**: Stage-level progress tracking and reporting
- **Kubernetes Events**: Integration with Kubernetes event system
- **Structured Logging**: JSON-formatted logs for log aggregation
- **Status Files**: Progress and status file generation for monitoring

#### Multi-Architecture in Kubernetes
- **Platform Emulation**: QEMU-based cross-platform builds in pods
- **Resource Scaling**: Resource requirements for multi-arch builds
- **Parallel Execution**: Concurrent platform builds within pods

### Performance Benchmark Tests (`performance_benchmark_test.go`)

#### Build Performance
- **Simple Builds**: Alpine, Ubuntu, Node.js application builds
- **Complex Builds**: Multi-stage, multi-architecture scenarios
- **Cache Performance**: Cold vs. warm cache build times
- **Concurrent Builds**: Multiple simultaneous build performance

#### Memory Usage
- **Peak Memory Tracking**: Memory consumption during builds
- **Memory Efficiency**: Comparison with other builders
- **Resource Limits**: Build performance under memory constraints
- **Garbage Collection**: Memory cleanup and optimization

#### Tool Comparison
- **OSSB vs. Kaniko**: Performance comparison across scenarios
- **OSSB vs. BuildKit**: Speed and resource usage comparison
- **Feature Parity**: Functionality comparison matrix
- **Scalability Testing**: Performance under load

## Test Environment Setup

### Prerequisites

#### Required Tools
```bash
# Go development environment
go version  # >= 1.21

# Container runtime (one of)
docker --version
podman --version

# Kubernetes access (optional)
kubectl version
kubectl cluster-info
```

#### Optional Tools for Comparison
```bash
# Kaniko for performance comparison
kaniko version

# BuildKit for performance comparison
docker buildx version
```

### Environment Variables

#### Registry Configuration
```bash
# Docker Hub credentials
export DOCKER_HUB_USERNAME="your-username"
export DOCKER_HUB_PASSWORD="your-password"

# Private registry
export PRIVATE_REGISTRY_URL="registry.example.com"
export PRIVATE_REGISTRY_USERNAME="username"
export PRIVATE_REGISTRY_PASSWORD="password"

# Google Container Registry
export GOOGLE_APPLICATION_CREDENTIALS="/path/to/service-account.json"

# Test registry for integration tests
export TEST_REGISTRY_URL="localhost:5000"
```

#### Kubernetes Configuration
```bash
# Kubernetes environment (auto-detected in pods)
export KUBERNETES_SERVICE_HOST="kubernetes.default.svc"
export POD_NAMESPACE="default"
export JOB_NAME="ossb-test-job"
export POD_NAME="ossb-test-pod"
```

#### Test Configuration
```bash
# Test behavior
export OSSB_DEBUG="true"
export OSSB_CACHE_DIR="/tmp/ossb-test-cache"
export TIMEOUT="30m"
export PARALLEL="4"
export VERBOSE="true"
```

## Running Tests

### Quick Start

```bash
# Run all integration tests
./run_integration_tests.sh

# Run specific test categories
./run_integration_tests.sh --unit
./run_integration_tests.sh --integration
./run_integration_tests.sh --registry
./run_integration_tests.sh --kubernetes
./run_integration_tests.sh --performance
```

### Manual Test Execution

#### Unit Tests
```bash
# Run all unit tests
go test -v ./...

# Run specific package tests
go test -v ./engine/...
go test -v ./registry/...
go test -v ./k8s/...
```

#### Integration Tests
```bash
# Run integration tests with build tag
go test -tags=integration -v ./integration_test.go
go test -tags=integration -v ./registry_integration_test.go
go test -tags=integration -v ./kubernetes_integration_test.go
go test -tags=integration -v ./performance_benchmark_test.go

# Run with timeout
go test -tags=integration -timeout=30m -v ./integration_test.go
```

#### Component-Specific Integration Tests
```bash
# Registry integration tests
go test -tags=integration -v ./registry/...

# Kubernetes integration tests
go test -tags=integration -v ./k8s/...

# Executor integration tests
go test -tags=integration -v ./executors/...

# Multi-stage integration tests
go test -tags=integration -v ./frontends/dockerfile/...
```

### Kubernetes Testing

#### Local Kubernetes Cluster
```bash
# Start local cluster (kind/minikube)
kind create cluster --name ossb-test
kubectl cluster-info --context kind-ossb-test

# Run Kubernetes tests
export KUBECONFIG="$(kind get kubeconfig-path --name ossb-test)"
./run_integration_tests.sh --kubernetes
```

#### In-Cluster Testing
```bash
# Create test job
kubectl apply -f k8s/ossb-job.yaml

# Monitor job execution
kubectl logs -f job/ossb-integration-test

# Check job status
kubectl get jobs
kubectl describe job ossb-integration-test
```

### Performance Testing

#### Benchmark Execution
```bash
# Run performance benchmarks
./run_integration_tests.sh --performance

# Run specific benchmarks
go test -tags=integration -bench=. -v ./performance_benchmark_test.go

# Generate performance report
go test -tags=integration -bench=. -benchmem -cpuprofile=cpu.prof -memprofile=mem.prof ./performance_benchmark_test.go
```

#### Comparison with Other Tools
```bash
# Ensure comparison tools are available
which kaniko
docker buildx version

# Run comparative benchmarks
export KANIKO_AVAILABLE=true
export BUILDKIT_AVAILABLE=true
./run_integration_tests.sh --performance
```

## Test Scenarios

### End-to-End Build Scenarios

#### Simple Application Builds
1. **Alpine Base**: Minimal Linux distribution with basic utilities
2. **Ubuntu Application**: Full Linux distribution with package management
3. **Node.js Web App**: JavaScript application with npm dependencies
4. **Go Application**: Compiled language with multi-stage build
5. **Python Application**: Interpreted language with pip dependencies

#### Complex Build Patterns
1. **Multi-Stage Builds**: Separate build and runtime environments
2. **Build Arguments**: Parameterized builds with ARG instructions
3. **Environment Configuration**: ENV variables and configuration files
4. **File Operations**: COPY, ADD with various patterns and permissions
5. **Network Configuration**: EXPOSE ports and health checks

#### Multi-Architecture Scenarios
1. **Cross-Platform Base Images**: Different architectures for same image
2. **Architecture-Specific Commands**: Platform-dependent operations
3. **Emulation Testing**: QEMU-based cross-compilation
4. **Manifest List Creation**: Multi-arch image distribution

### Registry Integration Scenarios

#### Public Registry Testing
1. **Docker Hub Access**: Official and community images
2. **Anonymous Pulls**: Rate limiting and public image access
3. **Authenticated Access**: Private repositories and organizations
4. **Registry Mirrors**: CDN and mirror registry usage

#### Private Registry Testing
1. **Self-Hosted Registries**: Harbor, Nexus, private Docker registries
2. **Cloud Registries**: GCR, ECR, ACR integration
3. **Authentication Methods**: Basic auth, tokens, service accounts
4. **TLS Configuration**: Certificate handling and insecure registries

#### Registry Operations
1. **Image Pulling**: Manifest parsing, layer downloading, caching
2. **Image Pushing**: Layer uploading, manifest creation, progress reporting
3. **Multi-Arch Operations**: Manifest list handling, platform selection
4. **Error Handling**: Network failures, authentication errors, retries

### Kubernetes Integration Scenarios

#### Pod Execution
1. **Job Lifecycle**: Start, progress, completion, cleanup
2. **Resource Management**: CPU, memory, storage limits
3. **Security Context**: Rootless execution, user namespaces
4. **Network Isolation**: Pod networking, service discovery

#### Configuration Management
1. **Secret Mounting**: Registry credentials, build secrets
2. **ConfigMap Usage**: Build context, configuration files
3. **Volume Management**: Persistent storage, temporary volumes
4. **Environment Variables**: Pod metadata, configuration injection

#### Monitoring and Observability
1. **Progress Reporting**: Build stage tracking, percentage completion
2. **Structured Logging**: JSON logs, log aggregation compatibility
3. **Event Generation**: Kubernetes events for build lifecycle
4. **Status Reporting**: Job status, exit codes, error categorization

### Performance Testing Scenarios

#### Build Performance
1. **Cold Cache Builds**: First-time builds without cache
2. **Warm Cache Builds**: Subsequent builds with layer cache
3. **Incremental Builds**: Small changes with maximum cache reuse
4. **Large Image Builds**: Complex applications with many layers

#### Resource Usage
1. **Memory Consumption**: Peak memory usage during builds
2. **CPU Utilization**: Processing efficiency and parallelization
3. **Disk I/O**: Layer operations, cache management
4. **Network Usage**: Registry operations, image transfers

#### Scalability Testing
1. **Concurrent Builds**: Multiple simultaneous build jobs
2. **Large Context Builds**: Builds with large build contexts
3. **Multi-Platform Builds**: Resource usage for cross-platform builds
4. **Cache Efficiency**: Cache hit rates and storage optimization

## Test Data and Fixtures

### Dockerfile Templates

#### Simple Applications
```dockerfile
# Alpine base
FROM alpine:latest
RUN apk add --no-cache curl
COPY app.sh /app.sh
RUN chmod +x /app.sh
CMD ["/app.sh"]

# Ubuntu with packages
FROM ubuntu:20.04
RUN apt-get update && apt-get install -y curl wget
COPY app /usr/local/bin/app
CMD ["app"]

# Node.js application
FROM node:18-alpine
WORKDIR /app
COPY package*.json ./
RUN npm ci --only=production
COPY . .
EXPOSE 3000
CMD ["npm", "start"]
```

#### Multi-Stage Builds
```dockerfile
# Go application with multi-stage
FROM golang:1.21-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -o main .

FROM alpine:latest
RUN apk --no-cache add ca-certificates
WORKDIR /root/
COPY --from=builder /app/main .
CMD ["./main"]
```

### Test Data Files

#### Application Code
- **Node.js**: Express server with package.json
- **Go**: HTTP server with go.mod
- **Python**: Flask application with requirements.txt
- **Shell Scripts**: Utility scripts for testing

#### Configuration Files
- **Registry Configs**: Docker config.json, registry mirrors
- **Kubernetes Manifests**: Jobs, secrets, config maps
- **Build Contexts**: Various file structures and patterns

### Mock Services

#### Registry Mock
- **Local Registry**: Docker registry:2 container
- **Authentication**: Basic auth, token auth simulation
- **Rate Limiting**: Simulated rate limiting scenarios
- **Error Injection**: Network failures, authentication errors

#### Kubernetes Mock
- **Secret Simulation**: Mock secret mounting
- **ConfigMap Simulation**: Mock configuration injection
- **Resource Limits**: Simulated resource constraints
- **Event Generation**: Mock Kubernetes events

## Continuous Integration

### GitHub Actions Integration

```yaml
name: Integration Tests
on: [push, pull_request]

jobs:
  integration:
    runs-on: ubuntu-latest
    strategy:
      matrix:
        test-type: [unit, integration, registry, performance]
    
    steps:
    - uses: actions/checkout@v3
    - uses: actions/setup-go@v3
      with:
        go-version: '1.21'
    
    - name: Run Integration Tests
      run: ./run_integration_tests.sh --${{ matrix.test-type }}
      env:
        DOCKER_HUB_USERNAME: ${{ secrets.DOCKER_HUB_USERNAME }}
        DOCKER_HUB_PASSWORD: ${{ secrets.DOCKER_HUB_PASSWORD }}
    
    - name: Upload Test Results
      uses: actions/upload-artifact@v3
      with:
        name: test-results-${{ matrix.test-type }}
        path: test-results/
```

### Jenkins Pipeline

```groovy
pipeline {
    agent any
    
    environment {
        DOCKER_HUB_USERNAME = credentials('docker-hub-username')
        DOCKER_HUB_PASSWORD = credentials('docker-hub-password')
    }
    
    stages {
        stage('Unit Tests') {
            steps {
                sh './run_integration_tests.sh --unit'
            }
        }
        
        stage('Integration Tests') {
            parallel {
                stage('Registry Tests') {
                    steps {
                        sh './run_integration_tests.sh --registry'
                    }
                }
                stage('Kubernetes Tests') {
                    when {
                        expression { env.BRANCH_NAME == 'main' }
                    }
                    steps {
                        sh './run_integration_tests.sh --kubernetes'
                    }
                }
            }
        }
        
        stage('Performance Tests') {
            when {
                expression { env.BRANCH_NAME == 'main' }
            }
            steps {
                sh './run_integration_tests.sh --performance'
            }
        }
    }
    
    post {
        always {
            archiveArtifacts artifacts: 'test-results/**/*', fingerprint: true
            publishTestResults testResultsPattern: 'test-results/*.xml'
        }
    }
}
```

## Troubleshooting

### Common Issues

#### Test Environment
- **Container Runtime**: Ensure Docker or Podman is running
- **Kubernetes Access**: Verify kubectl configuration and cluster access
- **Registry Access**: Check network connectivity and credentials
- **Resource Limits**: Ensure sufficient memory and disk space

#### Test Failures
- **Timeout Issues**: Increase timeout values for slow environments
- **Network Failures**: Check firewall and proxy configurations
- **Authentication Errors**: Verify credentials and permissions
- **Resource Constraints**: Monitor memory and CPU usage

#### Performance Issues
- **Slow Builds**: Check cache configuration and network speed
- **Memory Usage**: Monitor memory consumption and garbage collection
- **Concurrent Limits**: Adjust parallelism based on system resources
- **Cache Efficiency**: Verify cache hit rates and storage optimization

### Debug Mode

```bash
# Enable debug logging
export OSSB_DEBUG=true
export VERBOSE=true

# Run with detailed output
./run_integration_tests.sh --verbose

# Check individual test logs
cat test-results/integration.log
cat test-results/registry.log
cat test-results/kubernetes.log
```

### Test Isolation

```bash
# Clean test environment
rm -rf test-cache test-results
docker system prune -f
podman system prune -f

# Run tests in isolation
./run_integration_tests.sh --cleanup-cache
```

## Contributing

### Adding New Tests

1. **Test Structure**: Follow existing patterns for test organization
2. **Error Handling**: Include proper error handling and cleanup
3. **Documentation**: Document test scenarios and expected outcomes
4. **CI Integration**: Ensure tests work in CI/CD environments

### Test Categories

- **Unit Tests**: Fast, isolated component tests
- **Integration Tests**: Component interaction tests
- **End-to-End Tests**: Complete workflow validation
- **Performance Tests**: Benchmarking and comparison tests

### Best Practices

1. **Test Isolation**: Each test should be independent
2. **Resource Cleanup**: Always clean up test resources
3. **Error Messages**: Provide clear, actionable error messages
4. **Test Data**: Use realistic but minimal test data
5. **Timeouts**: Set appropriate timeouts for different scenarios

This comprehensive integration test suite ensures OSSB's reliability, performance, and compatibility across various deployment scenarios and use cases.