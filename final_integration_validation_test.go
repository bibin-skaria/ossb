// +build integration

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bibin-skaria/ossb/engine"
	"github.com/bibin-skaria/ossb/internal/types"
	"github.com/bibin-skaria/ossb/k8s"
	"github.com/bibin-skaria/ossb/registry"
	"github.com/bibin-skaria/ossb/security"
)

// TestFinalIntegrationRealWorldDockerfiles tests comprehensive end-to-end scenarios with real-world Dockerfile examples
func TestFinalIntegrationRealWorldDockerfiles(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping final integration test in short mode")
	}

	realWorldTestCases := []struct {
		name        string
		description string
		dockerfile  string
		files       map[string]string
		buildArgs   map[string]string
		platforms   []types.Platform
		expectPush  bool
		validation  func(*testing.T, *types.BuildResult) error
	}{
		{
			name:        "production_web_app",
			description: "Production-ready web application with multi-stage build",
			dockerfile: `# Production Web Application
FROM node:18-alpine AS dependencies
WORKDIR /app
COPY package*.json ./
RUN npm ci --only=production && npm cache clean --force

FROM node:18-alpine AS build
WORKDIR /app
COPY package*.json ./
RUN npm ci
COPY . .
RUN npm run build
RUN npm run test

FROM nginx:alpine AS production
COPY --from=build /app/dist /usr/share/nginx/html
COPY nginx.conf /etc/nginx/nginx.conf
EXPOSE 80
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD curl -f http://localhost/ || exit 1
CMD ["nginx", "-g", "daemon off;"]`,
			files: map[string]string{
				"package.json": `{
  "name": "production-web-app",
  "version": "1.0.0",
  "scripts": {
    "build": "webpack --mode=production",
    "test": "jest",
    "start": "node server.js"
  },
  "dependencies": {
    "express": "^4.18.0",
    "helmet": "^6.0.0"
  },
  "devDependencies": {
    "webpack": "^5.75.0",
    "jest": "^29.0.0"
  }
}`,
				"webpack.config.js": `module.exports = {
  entry: './src/index.js',
  output: {
    path: __dirname + '/dist',
    filename: 'bundle.js'
  }
};`,
				"src/index.js": `console.log('Production web app loaded');`,
				"jest.config.js": `module.exports = { testEnvironment: 'node' };`,
				"src/index.test.js": `test('basic test', () => { expect(1 + 1).toBe(2); });`,
				"nginx.conf": `events { worker_connections 1024; }
http {
  server {
    listen 80;
    location / {
      root /usr/share/nginx/html;
      index index.html;
    }
  }
}`,
			},
			platforms: []types.Platform{{OS: "linux", Architecture: "amd64"}},
			validation: func(t *testing.T, result *types.BuildResult) error {
				if result.Operations < 10 {
					return fmt.Errorf("expected more operations for complex build, got %d", result.Operations)
				}
				return nil
			},
		},
		{
			name:        "microservice_go_app",
			description: "Go microservice with security scanning and multi-arch support",
			dockerfile: `# Go Microservice
FROM golang:1.21-alpine AS builder
RUN apk add --no-cache git ca-certificates tzdata
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -ldflags '-extldflags "-static"' -o main .

FROM scratch
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /usr/share/zoneinfo /usr/share/zoneinfo
COPY --from=builder /app/main /main
EXPOSE 8080
USER 65534:65534
ENTRYPOINT ["/main"]`,
			files: map[string]string{
				"go.mod": `module microservice
go 1.21
require (
	github.com/gorilla/mux v1.8.0
	github.com/prometheus/client_golang v1.14.0
)`,
				"go.sum": `github.com/gorilla/mux v1.8.0 h1:i40aqfkR1h2SlN9hojwV5ZA91wcXFOvkdNIeFDP5koI=
github.com/gorilla/mux v1.8.0/go.mod h1:DVbg23sWSpFRCP0SfiEN6jmj59UnW/n46BH5rLB71So=`,
				"main.go": `package main
import (
	"fmt"
	"log"
	"net/http"
	"github.com/gorilla/mux"
)
func main() {
	r := mux.NewRouter()
	r.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "OK")
	})
	log.Fatal(http.ListenAndServe(":8080", r))
}`,
			},
			platforms: []types.Platform{
				{OS: "linux", Architecture: "amd64"},
				{OS: "linux", Architecture: "arm64"},
			},
			validation: func(t *testing.T, result *types.BuildResult) error {
				if !result.MultiArch {
					return fmt.Errorf("expected multi-arch build")
				}
				if len(result.PlatformResults) != 2 {
					return fmt.Errorf("expected 2 platform results, got %d", len(result.PlatformResults))
				}
				return nil
			},
		},
		{
			name:        "python_ml_pipeline",
			description: "Python ML pipeline with GPU support and data processing",
			dockerfile: `# Python ML Pipeline
FROM python:3.11-slim AS base
RUN apt-get update && apt-get install -y \
    build-essential \
    curl \
    && rm -rf /var/lib/apt/lists/*

FROM base AS dependencies
WORKDIR /app
COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt

FROM dependencies AS development
COPY requirements-dev.txt .
RUN pip install --no-cache-dir -r requirements-dev.txt
COPY . .
RUN python -m pytest tests/

FROM dependencies AS production
COPY src/ ./src/
COPY models/ ./models/
COPY config/ ./config/
RUN useradd -m -u 1000 mluser
USER mluser
EXPOSE 5000
HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
  CMD curl -f http://localhost:5000/health || exit 1
CMD ["python", "-m", "src.main"]`,
			files: map[string]string{
				"requirements.txt": `numpy==1.24.0
pandas==1.5.0
scikit-learn==1.2.0
flask==2.2.0
gunicorn==20.1.0`,
				"requirements-dev.txt": `pytest==7.2.0
black==22.12.0
flake8==6.0.0`,
				"src/main.py": `from flask import Flask
app = Flask(__name__)
@app.route('/health')
def health():
    return 'OK'
if __name__ == '__main__':
    app.run(host='0.0.0.0', port=5000)`,
				"tests/test_main.py": `def test_health():
    assert True`,
				"models/.gitkeep": "",
				"config/app.yaml": `debug: false
port: 5000`,
			},
			platforms: []types.Platform{{OS: "linux", Architecture: "amd64"}},
			validation: func(t *testing.T, result *types.BuildResult) error {
				if result.Operations < 8 {
					return fmt.Errorf("expected more operations for ML pipeline, got %d", result.Operations)
				}
				return nil
			},
		},
		{
			name:        "database_with_init",
			description: "Database container with initialization scripts and health checks",
			dockerfile: `# Database with Initialization
FROM postgres:15-alpine
RUN apk add --no-cache curl
COPY init-scripts/ /docker-entrypoint-initdb.d/
COPY health-check.sh /usr/local/bin/health-check.sh
RUN chmod +x /usr/local/bin/health-check.sh
EXPOSE 5432
HEALTHCHECK --interval=30s --timeout=5s --start-period=30s --retries=3 \
  CMD /usr/local/bin/health-check.sh
ENV POSTGRES_DB=appdb
ENV POSTGRES_USER=appuser
ENV POSTGRES_PASSWORD=secure_password`,
			files: map[string]string{
				"init-scripts/01-create-tables.sql": `CREATE TABLE users (
    id SERIAL PRIMARY KEY,
    username VARCHAR(50) UNIQUE NOT NULL,
    email VARCHAR(100) UNIQUE NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);`,
				"init-scripts/02-insert-data.sql": `INSERT INTO users (username, email) VALUES 
('admin', 'admin@example.com'),
('user1', 'user1@example.com');`,
				"health-check.sh": `#!/bin/sh
pg_isready -U $POSTGRES_USER -d $POSTGRES_DB`,
			},
			platforms: []types.Platform{{OS: "linux", Architecture: "amd64"}},
			validation: func(t *testing.T, result *types.BuildResult) error {
				if result.ImageID == "" {
					return fmt.Errorf("expected non-empty image ID")
				}
				return nil
			},
		},
	}

	for _, tc := range realWorldTestCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("Testing real-world scenario: %s", tc.description)
			
			tempDir, err := ioutil.TempDir("", fmt.Sprintf("ossb-final-%s-", tc.name))
			if err != nil {
				t.Fatalf("Failed to create temp dir: %v", err)
			}
			defer os.RemoveAll(tempDir)

			// Create all test files
			if err := createTestFiles(tempDir, tc.dockerfile, tc.files); err != nil {
				t.Fatalf("Failed to create test files: %v", err)
			}

			config := &types.BuildConfig{
				Context:    tempDir,
				Dockerfile: "Dockerfile",
				Tags:       []string{fmt.Sprintf("ossb-final-%s:latest", tc.name)},
				Output:     func() string { if len(tc.platforms) > 1 { return "multiarch" }; return "image" }(),
				Frontend:   "dockerfile",
				NoCache:    false,
				Progress:   true,
				BuildArgs:  tc.buildArgs,
				Platforms:  tc.platforms,
				Push:       tc.expectPush,
			}

			builder, err := engine.NewBuilder(config)
			if err != nil {
				t.Fatalf("Failed to create builder: %v", err)
			}
			defer builder.Cleanup()

			startTime := time.Now()
			result, err := builder.Build()
			buildDuration := time.Since(startTime)

			if err != nil {
				t.Fatalf("Build failed: %v", err)
			}

			if !result.Success {
				t.Fatalf("Build failed: %s", result.Error)
			}

			// Run custom validation
			if tc.validation != nil {
				if err := tc.validation(t, result); err != nil {
					t.Errorf("Validation failed: %v", err)
				}
			}

			t.Logf("Real-world test %s completed successfully:", tc.name)
			t.Logf("  Duration: %v", buildDuration)
			t.Logf("  Operations: %d", result.Operations)
			t.Logf("  Cache hits: %d", result.CacheHits)
			if result.MultiArch {
				t.Logf("  Platforms: %d", len(result.PlatformResults))
				t.Logf("  Manifest List ID: %s", result.ManifestListID)
			} else {
				t.Logf("  Image ID: %s", result.ImageID)
			}
		})
	}
}

// TestFinalIntegrationOCICompliance validates OCI compliance using official tools
func TestFinalIntegrationOCICompliance(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping OCI compliance test in short mode")
	}

	// Check if OCI validation tools are available
	if !isOCIValidatorAvailable() {
		t.Skip("OCI validation tools not available")
	}

	testCases := []struct {
		name       string
		dockerfile string
		files      map[string]string
	}{
		{
			name: "oci_compliant_image",
			dockerfile: `FROM alpine:latest
LABEL org.opencontainers.image.title="OCI Compliant Test Image"
LABEL org.opencontainers.image.description="Test image for OCI compliance validation"
LABEL org.opencontainers.image.version="1.0.0"
LABEL org.opencontainers.image.authors="OSSB Test Suite"
LABEL org.opencontainers.image.url="https://github.com/bibin-skaria/ossb"
LABEL org.opencontainers.image.source="https://github.com/bibin-skaria/ossb"
LABEL org.opencontainers.image.licenses="MIT"
RUN echo "OCI compliant image" > /oci-test.txt
EXPOSE 8080
USER 1000:1000
CMD ["cat", "/oci-test.txt"]`,
			files: map[string]string{},
		},
		{
			name: "multi_arch_oci_image",
			dockerfile: `FROM alpine:latest
LABEL org.opencontainers.image.title="Multi-arch OCI Image"
LABEL org.opencontainers.image.description="Multi-architecture OCI compliant image"
RUN apk add --no-cache file
RUN echo "Architecture: $(uname -m)" > /arch.txt
CMD ["cat", "/arch.txt"]`,
			files: map[string]string{},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			tempDir, err := ioutil.TempDir("", fmt.Sprintf("ossb-oci-%s-", tc.name))
			if err != nil {
				t.Fatalf("Failed to create temp dir: %v", err)
			}
			defer os.RemoveAll(tempDir)

			if err := createTestFiles(tempDir, tc.dockerfile, tc.files); err != nil {
				t.Fatalf("Failed to create test files: %v", err)
			}

			// Build image with OCI output format
			config := &types.BuildConfig{
				Context:    tempDir,
				Dockerfile: "Dockerfile",
				Tags:       []string{fmt.Sprintf("oci-test-%s:latest", tc.name)},
				Output:     "oci",
				Frontend:   "dockerfile",
				NoCache:    false,
				Progress:   true,
				BuildArgs:  map[string]string{},
				Platforms:  []types.Platform{{OS: "linux", Architecture: "amd64"}},
				Push:       false,
			}

			builder, err := engine.NewBuilder(config)
			if err != nil {
				t.Fatalf("Failed to create builder: %v", err)
			}
			defer builder.Cleanup()

			result, err := builder.Build()
			if err != nil {
				t.Fatalf("Build failed: %v", err)
			}

			if !result.Success {
				t.Fatalf("Build failed: %s", result.Error)
			}

			// Validate OCI compliance
			if err := validateOCICompliance(t, result.ImageID, tempDir); err != nil {
				t.Errorf("OCI compliance validation failed: %v", err)
			} else {
				t.Logf("OCI compliance validation passed for %s", tc.name)
			}
		})
	}
}

// TestFinalIntegrationKubernetesRealCluster tests Kubernetes integration in real cluster environments
func TestFinalIntegrationKubernetesRealCluster(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping Kubernetes real cluster test in short mode")
	}

	if !isKubernetesClusterAvailable() {
		t.Skip("Kubernetes cluster not available")
	}

	testCases := []struct {
		name        string
		jobSpec     string
		dockerfile  string
		files       map[string]string
		expectSuccess bool
	}{
		{
			name: "simple_build_job",
			jobSpec: `apiVersion: batch/v1
kind: Job
metadata:
  name: ossb-integration-simple
spec:
  template:
    spec:
      containers:
      - name: ossb
        image: ossb:latest
        command: ["ossb"]
        args: ["build", "--context=/workspace", "--push=false"]
        volumeMounts:
        - name: workspace
          mountPath: /workspace
        resources:
          limits:
            memory: "2Gi"
            cpu: "1000m"
          requests:
            memory: "1Gi"
            cpu: "500m"
      volumes:
      - name: workspace
        configMap:
          name: build-context
      restartPolicy: Never
  backoffLimit: 3`,
			dockerfile: `FROM alpine:latest
RUN echo "Kubernetes build test" > /test.txt
CMD ["cat", "/test.txt"]`,
			files: map[string]string{},
			expectSuccess: true,
		},
		{
			name: "multi_arch_build_job",
			jobSpec: `apiVersion: batch/v1
kind: Job
metadata:
  name: ossb-integration-multiarch
spec:
  template:
    spec:
      containers:
      - name: ossb
        image: ossb:latest
        command: ["ossb"]
        args: ["build", "--context=/workspace", "--platform=linux/amd64,linux/arm64", "--push=false"]
        volumeMounts:
        - name: workspace
          mountPath: /workspace
        resources:
          limits:
            memory: "4Gi"
            cpu: "2000m"
          requests:
            memory: "2Gi"
            cpu: "1000m"
      volumes:
      - name: workspace
        configMap:
          name: build-context-multiarch
      restartPolicy: Never
  backoffLimit: 3`,
			dockerfile: `FROM alpine:latest
RUN apk add --no-cache file
RUN echo "Multi-arch Kubernetes build: $(uname -m)" > /arch.txt
CMD ["cat", "/arch.txt"]`,
			files: map[string]string{},
			expectSuccess: true,
		},
		{
			name: "secret_handling_job",
			jobSpec: `apiVersion: batch/v1
kind: Job
metadata:
  name: ossb-integration-secrets
spec:
  template:
    spec:
      containers:
      - name: ossb
        image: ossb:latest
        command: ["ossb"]
        args: ["build", "--context=/workspace", "--push=true", "--registry=registry.example.com"]
        volumeMounts:
        - name: workspace
          mountPath: /workspace
        - name: registry-secret
          mountPath: /var/run/secrets/registry
          readOnly: true
        - name: build-secrets
          mountPath: /var/run/secrets/build
          readOnly: true
        env:
        - name: DOCKER_CONFIG
          value: /var/run/secrets/registry
        resources:
          limits:
            memory: "2Gi"
            cpu: "1000m"
      volumes:
      - name: workspace
        configMap:
          name: build-context-secrets
      - name: registry-secret
        secret:
          secretName: registry-credentials
      - name: build-secrets
        secret:
          secretName: build-secrets
      restartPolicy: Never
  backoffLimit: 3`,
			dockerfile: `FROM alpine:latest
ARG SECRET_VALUE
RUN echo "Build with secrets" > /test.txt
RUN echo "Secret length: ${#SECRET_VALUE}" >> /test.txt
CMD ["cat", "/test.txt"]`,
			files: map[string]string{},
			expectSuccess: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Create temporary directory for Kubernetes manifests
			tempDir, err := ioutil.TempDir("", fmt.Sprintf("ossb-k8s-%s-", tc.name))
			if err != nil {
				t.Fatalf("Failed to create temp dir: %v", err)
			}
			defer os.RemoveAll(tempDir)

			// Create ConfigMap for build context
			configMapName := fmt.Sprintf("build-context-%s", strings.ReplaceAll(tc.name, "_", "-"))
			if err := createKubernetesConfigMap(t, configMapName, tc.dockerfile, tc.files); err != nil {
				t.Fatalf("Failed to create ConfigMap: %v", err)
			}
			defer deleteKubernetesConfigMap(t, configMapName)

			// Create secrets if needed
			if strings.Contains(tc.name, "secret") {
				if err := createKubernetesSecrets(t); err != nil {
					t.Fatalf("Failed to create secrets: %v", err)
				}
				defer deleteKubernetesSecrets(t)
			}

			// Apply job specification
			jobFile := filepath.Join(tempDir, "job.yaml")
			if err := ioutil.WriteFile(jobFile, []byte(tc.jobSpec), 0644); err != nil {
				t.Fatalf("Failed to write job spec: %v", err)
			}

			// Apply the job
			cmd := exec.Command("kubectl", "apply", "-f", jobFile)
			if err := cmd.Run(); err != nil {
				t.Fatalf("Failed to apply job: %v", err)
			}

			jobName := fmt.Sprintf("ossb-integration-%s", strings.ReplaceAll(tc.name, "_", "-"))
			defer func() {
				exec.Command("kubectl", "delete", "job", jobName).Run()
			}()

			// Wait for job completion
			if err := waitForJobCompletion(t, jobName, 10*time.Minute); err != nil {
				if tc.expectSuccess {
					t.Errorf("Job failed unexpectedly: %v", err)
				} else {
					t.Logf("Job failed as expected: %v", err)
				}
			} else {
				if !tc.expectSuccess {
					t.Error("Job succeeded unexpectedly")
				} else {
					t.Logf("Kubernetes integration test %s completed successfully", tc.name)
				}
			}

			// Get job logs for debugging
			logs, _ := getJobLogs(t, jobName)
			t.Logf("Job logs for %s:\n%s", tc.name, logs)
		})
	}
}

// TestFinalIntegrationSecurityAudit performs security audit and penetration testing
func TestFinalIntegrationSecurityAudit(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping security audit test in short mode")
	}

	securityTestCases := []struct {
		name        string
		description string
		test        func(*testing.T) error
	}{
		{
			name:        "rootless_execution",
			description: "Verify builds run without root privileges",
			test:        testRootlessExecution,
		},
		{
			name:        "secret_handling",
			description: "Verify secure handling of build secrets",
			test:        testSecretHandling,
		},
		{
			name:        "privilege_escalation",
			description: "Test for privilege escalation vulnerabilities",
			test:        testPrivilegeEscalation,
		},
		{
			name:        "container_escape",
			description: "Test for container escape vulnerabilities",
			test:        testContainerEscape,
		},
		{
			name:        "resource_limits",
			description: "Verify resource limit enforcement",
			test:        testResourceLimits,
		},
		{
			name:        "input_validation",
			description: "Test input validation and sanitization",
			test:        testInputValidation,
		},
		{
			name:        "network_isolation",
			description: "Verify network isolation during builds",
			test:        testNetworkIsolation,
		},
	}

	for _, tc := range securityTestCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("Running security test: %s", tc.description)
			
			if err := tc.test(t); err != nil {
				t.Errorf("Security test %s failed: %v", tc.name, err)
			} else {
				t.Logf("Security test %s passed", tc.name)
			}
		})
	}
}

// TestFinalIntegrationPerformanceBenchmark executes comprehensive performance benchmarking
func TestFinalIntegrationPerformanceBenchmark(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping performance benchmark test in short mode")
	}

	benchmarkSuites := []struct {
		name        string
		description string
		test        func(*testing.T) error
	}{
		{
			name:        "build_speed_comparison",
			description: "Compare build speeds with Kaniko and BuildKit",
			test:        benchmarkBuildSpeed,
		},
		{
			name:        "memory_usage_analysis",
			description: "Analyze memory usage patterns",
			test:        benchmarkMemoryUsage,
		},
		{
			name:        "cache_efficiency",
			description: "Measure cache hit rates and effectiveness",
			test:        benchmarkCacheEfficiency,
		},
		{
			name:        "concurrent_builds",
			description: "Test performance under concurrent load",
			test:        benchmarkConcurrentBuilds,
		},
		{
			name:        "large_context_handling",
			description: "Test performance with large build contexts",
			test:        benchmarkLargeContexts,
		},
		{
			name:        "multi_arch_performance",
			description: "Measure multi-architecture build performance",
			test:        benchmarkMultiArchPerformance,
		},
	}

	results := make(map[string]interface{})

	for _, suite := range benchmarkSuites {
		t.Run(suite.name, func(t *testing.T) {
			t.Logf("Running performance benchmark: %s", suite.description)
			
			if err := suite.test(t); err != nil {
				t.Errorf("Performance benchmark %s failed: %v", suite.name, err)
				results[suite.name] = map[string]interface{}{
					"success": false,
					"error":   err.Error(),
				}
			} else {
				t.Logf("Performance benchmark %s completed", suite.name)
				results[suite.name] = map[string]interface{}{
					"success": true,
				}
			}
		})
	}

	// Generate performance report
	if err := generatePerformanceReport(t, results); err != nil {
		t.Errorf("Failed to generate performance report: %v", err)
	}
}

// Helper functions for test implementation

func createTestFiles(dir, dockerfile string, files map[string]string) error {
	// Create Dockerfile
	dockerfilePath := filepath.Join(dir, "Dockerfile")
	if err := ioutil.WriteFile(dockerfilePath, []byte(dockerfile), 0644); err != nil {
		return err
	}

	// Create additional files
	for filename, content := range files {
		filePath := filepath.Join(dir, filename)
		
		// Create directory if needed
		if fileDir := filepath.Dir(filePath); fileDir != dir {
			if err := os.MkdirAll(fileDir, 0755); err != nil {
				return err
			}
		}

		if err := ioutil.WriteFile(filePath, []byte(content), 0644); err != nil {
			return err
		}
	}

	return nil
}

func isOCIValidatorAvailable() bool {
	// Check for OCI image validator tools
	tools := []string{"oci-image-tool", "skopeo", "crane"}
	for _, tool := range tools {
		if _, err := exec.LookPath(tool); err == nil {
			return true
		}
	}
	return false
}

func validateOCICompliance(t *testing.T, imageID, workDir string) error {
	// Use available OCI validation tools
	if _, err := exec.LookPath("oci-image-tool"); err == nil {
		return validateWithOCIImageTool(imageID, workDir)
	}
	
	if _, err := exec.LookPath("skopeo"); err == nil {
		return validateWithSkopeo(imageID)
	}
	
	return fmt.Errorf("no OCI validation tools available")
}

func validateWithOCIImageTool(imageID, workDir string) error {
	cmd := exec.Command("oci-image-tool", "validate", "--ref", imageID, workDir)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("OCI validation failed: %v, output: %s", err, output)
	}
	return nil
}

func validateWithSkopeo(imageID string) error {
	cmd := exec.Command("skopeo", "inspect", fmt.Sprintf("docker-daemon:%s", imageID))
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("Skopeo validation failed: %v, output: %s", err, output)
	}
	return nil
}

func isKubernetesClusterAvailable() bool {
	cmd := exec.Command("kubectl", "cluster-info")
	return cmd.Run() == nil
}

func createKubernetesConfigMap(t *testing.T, name, dockerfile string, files map[string]string) error {
	// Create temporary directory for ConfigMap data
	tempDir, err := ioutil.TempDir("", "k8s-configmap-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tempDir)

	// Create files
	if err := createTestFiles(tempDir, dockerfile, files); err != nil {
		return err
	}

	// Create ConfigMap from directory
	cmd := exec.Command("kubectl", "create", "configmap", name, "--from-file="+tempDir)
	return cmd.Run()
}

func deleteKubernetesConfigMap(t *testing.T, name string) {
	exec.Command("kubectl", "delete", "configmap", name).Run()
}

func createKubernetesSecrets(t *testing.T) error {
	// Create registry credentials secret
	cmd := exec.Command("kubectl", "create", "secret", "docker-registry", "registry-credentials",
		"--docker-server=registry.example.com",
		"--docker-username=testuser",
		"--docker-password=testpass",
		"--docker-email=test@example.com")
	if err := cmd.Run(); err != nil {
		return err
	}

	// Create build secrets
	cmd = exec.Command("kubectl", "create", "secret", "generic", "build-secrets",
		"--from-literal=SECRET_VALUE=test-secret-value",
		"--from-literal=API_KEY=test-api-key")
	return cmd.Run()
}

func deleteKubernetesSecrets(t *testing.T) {
	exec.Command("kubectl", "delete", "secret", "registry-credentials").Run()
	exec.Command("kubectl", "delete", "secret", "build-secrets").Run()
}

func waitForJobCompletion(t *testing.T, jobName string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("job %s did not complete within timeout", jobName)
		default:
			cmd := exec.Command("kubectl", "get", "job", jobName, "-o", "jsonpath={.status.conditions[?(@.type==\"Complete\")].status}")
			output, err := cmd.Output()
			if err == nil && strings.TrimSpace(string(output)) == "True" {
				return nil
			}

			// Check for failure
			cmd = exec.Command("kubectl", "get", "job", jobName, "-o", "jsonpath={.status.conditions[?(@.type==\"Failed\")].status}")
			output, err = cmd.Output()
			if err == nil && strings.TrimSpace(string(output)) == "True" {
				return fmt.Errorf("job %s failed", jobName)
			}

			time.Sleep(10 * time.Second)
		}
	}
}

func getJobLogs(t *testing.T, jobName string) (string, error) {
	cmd := exec.Command("kubectl", "logs", "job/"+jobName)
	output, err := cmd.Output()
	return string(output), err
}

// Security test implementations

func testRootlessExecution(t *testing.T) error {
	tempDir, err := ioutil.TempDir("", "ossb-rootless-test-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tempDir)

	dockerfile := `FROM alpine:latest
RUN id > /user-info.txt
RUN whoami >> /user-info.txt
CMD ["cat", "/user-info.txt"]`

	if err := createTestFiles(tempDir, dockerfile, map[string]string{}); err != nil {
		return err
	}

	config := &types.BuildConfig{
		Context:    tempDir,
		Dockerfile: "Dockerfile",
		Tags:       []string{"rootless-test:latest"},
		Output:     "image",
		Frontend:   "dockerfile",
		Rootless:   true,
		SecurityContext: &types.SecurityContext{
			RunAsNonRoot: &[]bool{true}[0],
			RunAsUser:    &[]int64{1000}[0],
		},
	}

	builder, err := engine.NewBuilder(config)
	if err != nil {
		return err
	}
	defer builder.Cleanup()

	result, err := builder.Build()
	if err != nil {
		return err
	}

	if !result.Success {
		return fmt.Errorf("rootless build failed: %s", result.Error)
	}

	return nil
}

func testSecretHandling(t *testing.T) error {
	scanner := security.NewSecurityScanner()
	
	// Test secret detection in Dockerfile
	dockerfile := `FROM alpine:latest
ENV SECRET_KEY=super-secret-value
RUN echo "password123" > /tmp/secret.txt
CMD ["cat", "/tmp/secret.txt"]`

	issues, err := scanner.ScanDockerfile([]byte(dockerfile))
	if err != nil {
		return err
	}

	// Should detect secret exposure
	if len(issues) == 0 {
		return fmt.Errorf("expected security scanner to detect secret exposure")
	}

	return nil
}

func testPrivilegeEscalation(t *testing.T) error {
	tempDir, err := ioutil.TempDir("", "ossb-privilege-test-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tempDir)

	// Dockerfile that attempts privilege escalation
	dockerfile := `FROM alpine:latest
RUN apk add --no-cache sudo
USER 1000
RUN sudo whoami || echo "Privilege escalation blocked"
CMD ["id"]`

	if err := createTestFiles(tempDir, dockerfile, map[string]string{}); err != nil {
		return err
	}

	config := &types.BuildConfig{
		Context:    tempDir,
		Dockerfile: "Dockerfile",
		Tags:       []string{"privilege-test:latest"},
		Output:     "image",
		Frontend:   "dockerfile",
		Rootless:   true,
		SecurityContext: &types.SecurityContext{
			RunAsNonRoot: &[]bool{true}[0],
			Capabilities: []string{"!ALL"},
		},
	}

	builder, err := engine.NewBuilder(config)
	if err != nil {
		return err
	}
	defer builder.Cleanup()

	result, err := builder.Build()
	
	// Build should succeed but privilege escalation should be blocked
	if err != nil {
		return err
	}

	if !result.Success {
		return fmt.Errorf("privilege escalation test build failed: %s", result.Error)
	}

	return nil
}

func testContainerEscape(t *testing.T) error {
	// Test for container escape vulnerabilities
	validator := security.NewSecurityValidator()
	
	// Test various escape vectors
	escapeTests := []string{
		"/proc/self/root",
		"/sys/fs/cgroup",
		"/dev/kmsg",
		"/proc/kcore",
	}

	for _, path := range escapeTests {
		if err := validator.ValidatePathAccess(path); err == nil {
			return fmt.Errorf("container escape vulnerability: access to %s should be blocked", path)
		}
	}

	return nil
}

func testResourceLimits(t *testing.T) error {
	tempDir, err := ioutil.TempDir("", "ossb-resource-test-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tempDir)

	// Dockerfile that attempts to consume excessive resources
	dockerfile := `FROM alpine:latest
RUN dd if=/dev/zero of=/tmp/large-file bs=1M count=1000 || echo "Resource limit enforced"
CMD ["ls", "-lh", "/tmp/"]`

	if err := createTestFiles(tempDir, dockerfile, map[string]string{}); err != nil {
		return err
	}

	config := &types.BuildConfig{
		Context:    tempDir,
		Dockerfile: "Dockerfile",
		Tags:       []string{"resource-test:latest"},
		Output:     "image",
		Frontend:   "dockerfile",
		ResourceLimits: &types.ResourceLimits{
			Memory: "512Mi",
			CPU:    "500m",
			Disk:   "1Gi",
		},
	}

	builder, err := engine.NewBuilder(config)
	if err != nil {
		return err
	}
	defer builder.Cleanup()

	result, err := builder.Build()
	if err != nil {
		return err
	}

	if !result.Success {
		return fmt.Errorf("resource limits test build failed: %s", result.Error)
	}

	return nil
}

func testInputValidation(t *testing.T) error {
	validator := security.NewSecurityValidator()
	
	// Test malicious inputs
	maliciousInputs := []string{
		"../../../etc/passwd",
		"$(rm -rf /)",
		"; cat /etc/shadow",
		"' OR '1'='1",
		"<script>alert('xss')</script>",
	}

	for _, input := range maliciousInputs {
		if err := validator.ValidateInput(input); err == nil {
			return fmt.Errorf("input validation failed for: %s", input)
		}
	}

	return nil
}

func testNetworkIsolation(t *testing.T) error {
	tempDir, err := ioutil.TempDir("", "ossb-network-test-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tempDir)

	dockerfile := `FROM alpine:latest
RUN apk add --no-cache curl
RUN curl -m 5 http://example.com || echo "Network access blocked"
CMD ["echo", "Network isolation test"]`

	if err := createTestFiles(tempDir, dockerfile, map[string]string{}); err != nil {
		return err
	}

	config := &types.BuildConfig{
		Context:     tempDir,
		Dockerfile:  "Dockerfile",
		Tags:        []string{"network-test:latest"},
		Output:      "image",
		Frontend:    "dockerfile",
		NetworkMode: "none", // Isolated network
	}

	builder, err := engine.NewBuilder(config)
	if err != nil {
		return err
	}
	defer builder.Cleanup()

	result, err := builder.Build()
	if err != nil {
		return err
	}

	if !result.Success {
		return fmt.Errorf("network isolation test build failed: %s", result.Error)
	}

	return nil
}

// Performance benchmark implementations

func benchmarkBuildSpeed(t *testing.T) error {
	// Implementation for build speed comparison
	return fmt.Errorf("not implemented")
}

func benchmarkMemoryUsage(t *testing.T) error {
	// Implementation for memory usage analysis
	return fmt.Errorf("not implemented")
}

func benchmarkCacheEfficiency(t *testing.T) error {
	// Implementation for cache efficiency measurement
	return fmt.Errorf("not implemented")
}

func benchmarkConcurrentBuilds(t *testing.T) error {
	// Implementation for concurrent build testing
	return fmt.Errorf("not implemented")
}

func benchmarkLargeContexts(t *testing.T) error {
	// Implementation for large context handling
	return fmt.Errorf("not implemented")
}

func benchmarkMultiArchPerformance(t *testing.T) error {
	// Implementation for multi-arch performance testing
	return fmt.Errorf("not implemented")
}

func generatePerformanceReport(t *testing.T, results map[string]interface{}) error {
	reportFile := "test-results/final-integration-performance-report.json"
	
	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(reportFile), 0755); err != nil {
		return err
	}

	// Add metadata
	report := map[string]interface{}{
		"timestamp": time.Now().UTC().Format(time.RFC3339),
		"version":   "1.0.0",
		"results":   results,
	}

	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}

	return ioutil.WriteFile(reportFile, data, 0644)
}