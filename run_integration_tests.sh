#!/bin/bash

# OSSB Integration Test Suite Runner
# This script runs comprehensive integration tests for OSSB

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Configuration
TIMEOUT=${TIMEOUT:-30m}
PARALLEL=${PARALLEL:-4}
VERBOSE=${VERBOSE:-false}
REGISTRY_URL=${REGISTRY_URL:-""}
DOCKER_HUB_USERNAME=${DOCKER_HUB_USERNAME:-""}
DOCKER_HUB_PASSWORD=${DOCKER_HUB_PASSWORD:-""}

# Test categories
RUN_UNIT=${RUN_UNIT:-true}
RUN_INTEGRATION=${RUN_INTEGRATION:-true}
RUN_REGISTRY=${RUN_REGISTRY:-true}
RUN_KUBERNETES=${RUN_KUBERNETES:-false}
RUN_PERFORMANCE=${RUN_PERFORMANCE:-false}

print_header() {
    echo -e "${BLUE}================================${NC}"
    echo -e "${BLUE}$1${NC}"
    echo -e "${BLUE}================================${NC}"
}

print_success() {
    echo -e "${GREEN}✓ $1${NC}"
}

print_warning() {
    echo -e "${YELLOW}⚠ $1${NC}"
}

print_error() {
    echo -e "${RED}✗ $1${NC}"
}

print_info() {
    echo -e "${BLUE}ℹ $1${NC}"
}

check_prerequisites() {
    print_header "Checking Prerequisites"
    
    # Check Go
    if ! command -v go &> /dev/null; then
        print_error "Go is not installed"
        exit 1
    fi
    print_success "Go $(go version | cut -d' ' -f3) found"
    
    # Check Docker/Podman
    if command -v podman &> /dev/null; then
        print_success "Podman found"
        CONTAINER_RUNTIME="podman"
    elif command -v docker &> /dev/null; then
        print_success "Docker found"
        CONTAINER_RUNTIME="docker"
    else
        print_warning "No container runtime found (Docker/Podman)"
        print_warning "Some integration tests will be skipped"
    fi
    
    # Check Kubernetes
    if command -v kubectl &> /dev/null && kubectl cluster-info &> /dev/null; then
        print_success "Kubernetes cluster access available"
        K8S_AVAILABLE=true
    else
        print_warning "Kubernetes cluster not available"
        print_warning "Kubernetes integration tests will be skipped"
        K8S_AVAILABLE=false
    fi
    
    # Check for comparison tools
    if command -v kaniko &> /dev/null; then
        print_success "Kaniko found for performance comparison"
        KANIKO_AVAILABLE=true
    else
        print_warning "Kaniko not found - performance comparison limited"
        KANIKO_AVAILABLE=false
    fi
    
    if command -v docker &> /dev/null && docker buildx version &> /dev/null; then
        print_success "BuildKit (docker buildx) found for performance comparison"
        BUILDKIT_AVAILABLE=true
    else
        print_warning "BuildKit not found - performance comparison limited"
        BUILDKIT_AVAILABLE=false
    fi
}

setup_test_environment() {
    print_header "Setting Up Test Environment"
    
    # Create test directories
    mkdir -p test-results
    mkdir -p test-cache
    
    # Set environment variables
    export OSSB_CACHE_DIR="$(pwd)/test-cache"
    export OSSB_DEBUG="true"
    
    # Set up registry credentials if provided
    if [[ -n "$DOCKER_HUB_USERNAME" && -n "$DOCKER_HUB_PASSWORD" ]]; then
        export DOCKER_HUB_USERNAME
        export DOCKER_HUB_PASSWORD
        print_success "Docker Hub credentials configured"
    fi
    
    if [[ -n "$REGISTRY_URL" ]]; then
        export TEST_REGISTRY_URL="$REGISTRY_URL"
        print_success "Test registry configured: $REGISTRY_URL"
    fi
    
    print_success "Test environment configured"
}

run_unit_tests() {
    if [[ "$RUN_UNIT" != "true" ]]; then
        return 0
    fi
    
    print_header "Running Unit Tests"
    
    local test_packages=(
        "./engine/..."
        "./executors/..."
        "./registry/..."
        "./k8s/..."
        "./layers/..."
        "./manifest/..."
        "./frontends/..."
    )
    
    for package in "${test_packages[@]}"; do
        print_info "Testing $package"
        if go test -timeout="$TIMEOUT" -v "$package" > "test-results/unit-$(basename "$package").log" 2>&1; then
            print_success "Unit tests passed for $package"
        else
            print_error "Unit tests failed for $package"
            if [[ "$VERBOSE" == "true" ]]; then
                cat "test-results/unit-$(basename "$package").log"
            fi
            return 1
        fi
    done
    
    print_success "All unit tests passed"
}

run_integration_tests() {
    if [[ "$RUN_INTEGRATION" != "true" ]]; then
        return 0
    fi
    
    print_header "Running Integration Tests"
    
    print_info "Running end-to-end integration tests..."
    if go test -tags=integration -timeout="$TIMEOUT" -v ./integration_test.go > test-results/integration.log 2>&1; then
        print_success "End-to-end integration tests passed"
    else
        print_error "End-to-end integration tests failed"
        if [[ "$VERBOSE" == "true" ]]; then
            cat test-results/integration.log
        fi
        return 1
    fi
}

run_registry_tests() {
    if [[ "$RUN_REGISTRY" != "true" ]]; then
        return 0
    fi
    
    print_header "Running Registry Integration Tests"
    
    print_info "Running registry integration tests..."
    if go test -tags=integration -timeout="$TIMEOUT" -v ./registry_integration_test.go > test-results/registry.log 2>&1; then
        print_success "Registry integration tests passed"
    else
        print_error "Registry integration tests failed"
        if [[ "$VERBOSE" == "true" ]]; then
            cat test-results/registry.log
        fi
        return 1
    fi
}

run_kubernetes_tests() {
    if [[ "$RUN_KUBERNETES" != "true" ]] || [[ "$K8S_AVAILABLE" != "true" ]]; then
        print_warning "Skipping Kubernetes tests (not enabled or cluster not available)"
        return 0
    fi
    
    print_header "Running Kubernetes Integration Tests"
    
    # Set up Kubernetes test environment
    export KUBERNETES_SERVICE_HOST="kubernetes.default.svc"
    export POD_NAMESPACE="default"
    export JOB_NAME="ossb-integration-test"
    export POD_NAME="ossb-test-pod"
    
    print_info "Running Kubernetes integration tests..."
    if go test -tags=integration -timeout="$TIMEOUT" -v ./kubernetes_integration_test.go > test-results/kubernetes.log 2>&1; then
        print_success "Kubernetes integration tests passed"
    else
        print_error "Kubernetes integration tests failed"
        if [[ "$VERBOSE" == "true" ]]; then
            cat test-results/kubernetes.log
        fi
        return 1
    fi
}

run_performance_tests() {
    if [[ "$RUN_PERFORMANCE" != "true" ]]; then
        print_warning "Skipping performance tests (not enabled)"
        return 0
    fi
    
    print_header "Running Performance Benchmark Tests"
    
    print_info "Running performance benchmarks..."
    if go test -tags=integration -timeout="$TIMEOUT" -v ./performance_benchmark_test.go > test-results/performance.log 2>&1; then
        print_success "Performance benchmark tests completed"
    else
        print_error "Performance benchmark tests failed"
        if [[ "$VERBOSE" == "true" ]]; then
            cat test-results/performance.log
        fi
        return 1
    fi
    
    # Run comprehensive performance benchmark script
    if [ -x "./scripts/performance-benchmark.sh" ]; then
        print_info "Running comprehensive performance benchmarks..."
        if ./scripts/performance-benchmark.sh --output test-results/performance-benchmark; then
            print_success "Comprehensive performance benchmarks completed"
        else
            print_warning "Comprehensive performance benchmarks failed"
        fi
    fi
}

run_specific_integration_tests() {
    print_header "Running Component-Specific Integration Tests"
    
    local components=(
        "registry"
        "k8s"
        "executors"
        "frontends/dockerfile"
        "manifest"
    )
    
    for component in "${components[@]}"; do
        print_info "Running integration tests for $component"
        if go test -tags=integration -timeout="$TIMEOUT" -v "./$component/..." > "test-results/integration-$component.log" 2>&1; then
            print_success "Integration tests passed for $component"
        else
            print_error "Integration tests failed for $component"
            if [[ "$VERBOSE" == "true" ]]; then
                cat "test-results/integration-$component.log"
            fi
        fi
    done
}

run_final_validation_tests() {
    print_header "Running Final Integration and Validation Tests"
    
    print_info "Running comprehensive final validation tests..."
    if go test -tags=integration -timeout="$TIMEOUT" -v ./final_integration_validation_test.go > test-results/final-validation.log 2>&1; then
        print_success "Final validation tests completed"
    else
        print_error "Final validation tests failed"
        if [[ "$VERBOSE" == "true" ]]; then
            cat test-results/final-validation.log
        fi
        return 1
    fi
    
    # Run OCI compliance validation
    if [ -x "./scripts/validate-oci-compliance.sh" ]; then
        print_info "Running OCI compliance validation..."
        if ./scripts/validate-oci-compliance.sh --check-tools > test-results/oci-validation.log 2>&1; then
            print_success "OCI compliance validation tools available"
        else
            print_warning "OCI compliance validation tools not available"
        fi
    fi
    
    # Run security audit
    if [ -x "./scripts/security-audit.sh" ]; then
        print_info "Running security audit..."
        if ./scripts/security-audit.sh --output test-results/security-audit > test-results/security-audit.log 2>&1; then
            print_success "Security audit completed"
        else
            print_warning "Security audit failed or found issues"
        fi
    fi
}

generate_test_report() {
    print_header "Generating Test Report"
    
    local report_file="test-results/test-report.md"
    
    cat > "$report_file" << EOF
# OSSB Integration Test Report

Generated on: $(date)

## Test Environment

- Go Version: $(go version)
- Container Runtime: ${CONTAINER_RUNTIME:-"Not Available"}
- Kubernetes Available: ${K8S_AVAILABLE:-false}
- Kaniko Available: ${KANIKO_AVAILABLE:-false}
- BuildKit Available: ${BUILDKIT_AVAILABLE:-false}

## Test Results

EOF
    
    # Add results for each test category
    for log_file in test-results/*.log; do
        if [[ -f "$log_file" ]]; then
            local test_name=$(basename "$log_file" .log)
            echo "### $test_name" >> "$report_file"
            echo "" >> "$report_file"
            
            if grep -q "PASS" "$log_file"; then
                echo "✅ **PASSED**" >> "$report_file"
            elif grep -q "FAIL" "$log_file"; then
                echo "❌ **FAILED**" >> "$report_file"
            else
                echo "⚠️ **INCOMPLETE**" >> "$report_file"
            fi
            
            echo "" >> "$report_file"
            echo "```" >> "$report_file"
            tail -20 "$log_file" >> "$report_file"
            echo "```" >> "$report_file"
            echo "" >> "$report_file"
        fi
    done
    
    print_success "Test report generated: $report_file"
}

cleanup() {
    print_header "Cleaning Up"
    
    # Clean up test containers
    if command -v docker &> /dev/null; then
        docker system prune -f --filter "label=ossb-test" &> /dev/null || true
    fi
    
    if command -v podman &> /dev/null; then
        podman system prune -f &> /dev/null || true
    fi
    
    # Clean up test cache if requested
    if [[ "${CLEANUP_CACHE:-false}" == "true" ]]; then
        rm -rf test-cache
        print_success "Test cache cleaned up"
    fi
    
    print_success "Cleanup completed"
}

show_usage() {
    cat << EOF
OSSB Integration Test Suite Runner

Usage: $0 [OPTIONS]

Options:
    -h, --help              Show this help message
    -v, --verbose           Enable verbose output
    -t, --timeout DURATION  Set test timeout (default: 30m)
    -p, --parallel N        Set parallel test execution (default: 4)
    
    --unit                  Run unit tests only
    --integration           Run integration tests only
    --registry              Run registry tests only
    --kubernetes            Run Kubernetes tests only
    --performance           Run performance tests only
    --all                   Run all tests (default)
    
    --registry-url URL      Set test registry URL
    --cleanup-cache         Clean up test cache after completion

Environment Variables:
    DOCKER_HUB_USERNAME     Docker Hub username for registry tests
    DOCKER_HUB_PASSWORD     Docker Hub password for registry tests
    PRIVATE_REGISTRY_URL    Private registry URL for testing
    PRIVATE_REGISTRY_USERNAME   Private registry username
    PRIVATE_REGISTRY_PASSWORD   Private registry password
    GOOGLE_APPLICATION_CREDENTIALS  Path to GCP service account key

Examples:
    # Run all tests
    $0
    
    # Run only unit tests
    $0 --unit
    
    # Run integration tests with verbose output
    $0 --integration --verbose
    
    # Run performance tests with custom registry
    $0 --performance --registry-url localhost:5000
    
    # Run Kubernetes tests (requires cluster access)
    $0 --kubernetes

EOF
}

main() {
    # Parse command line arguments
    while [[ $# -gt 0 ]]; do
        case $1 in
            -h|--help)
                show_usage
                exit 0
                ;;
            -v|--verbose)
                VERBOSE=true
                shift
                ;;
            -t|--timeout)
                TIMEOUT="$2"
                shift 2
                ;;
            -p|--parallel)
                PARALLEL="$2"
                shift 2
                ;;
            --unit)
                RUN_UNIT=true
                RUN_INTEGRATION=false
                RUN_REGISTRY=false
                RUN_KUBERNETES=false
                RUN_PERFORMANCE=false
                shift
                ;;
            --integration)
                RUN_UNIT=false
                RUN_INTEGRATION=true
                RUN_REGISTRY=false
                RUN_KUBERNETES=false
                RUN_PERFORMANCE=false
                shift
                ;;
            --registry)
                RUN_UNIT=false
                RUN_INTEGRATION=false
                RUN_REGISTRY=true
                RUN_KUBERNETES=false
                RUN_PERFORMANCE=false
                shift
                ;;
            --kubernetes)
                RUN_UNIT=false
                RUN_INTEGRATION=false
                RUN_REGISTRY=false
                RUN_KUBERNETES=true
                RUN_PERFORMANCE=false
                shift
                ;;
            --performance)
                RUN_UNIT=false
                RUN_INTEGRATION=false
                RUN_REGISTRY=false
                RUN_KUBERNETES=false
                RUN_PERFORMANCE=true
                shift
                ;;
            --all)
                RUN_UNIT=true
                RUN_INTEGRATION=true
                RUN_REGISTRY=true
                RUN_KUBERNETES=true
                RUN_PERFORMANCE=true
                shift
                ;;
            --registry-url)
                REGISTRY_URL="$2"
                shift 2
                ;;
            --cleanup-cache)
                CLEANUP_CACHE=true
                shift
                ;;
            *)
                print_error "Unknown option: $1"
                show_usage
                exit 1
                ;;
        esac
    done
    
    # Set up trap for cleanup
    trap cleanup EXIT
    
    print_header "OSSB Integration Test Suite"
    print_info "Timeout: $TIMEOUT"
    print_info "Parallel: $PARALLEL"
    print_info "Verbose: $VERBOSE"
    
    # Run test phases
    check_prerequisites
    setup_test_environment
    
    local exit_code=0
    
    run_unit_tests || exit_code=1
    run_integration_tests || exit_code=1
    run_registry_tests || exit_code=1
    run_kubernetes_tests || exit_code=1
    run_performance_tests || exit_code=1
    run_specific_integration_tests || exit_code=1
    run_final_validation_tests || exit_code=1
    
    generate_test_report
    
    if [[ $exit_code -eq 0 ]]; then
        print_success "All tests completed successfully!"
    else
        print_error "Some tests failed. Check test-results/ for details."
    fi
    
    exit $exit_code
}

# Run main function
main "$@"