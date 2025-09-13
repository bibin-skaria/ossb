#!/bin/bash

# Final Integration and Validation Test Runner for OSSB
# This script orchestrates comprehensive testing including real-world scenarios,
# OCI compliance validation, security auditing, and performance benchmarking

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

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

# Configuration
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
WORKSPACE_ROOT="$(cd "$SCRIPT_DIR" && pwd)"
CONFIG_FILE="${CONFIG_FILE:-test_config_final.yaml}"
OUTPUT_DIR="${OUTPUT_DIR:-./test-results/final-validation}"
TIMEOUT="${TIMEOUT:-45m}"
VERBOSE="${VERBOSE:-true}"

# Test execution tracking
TOTAL_TESTS=0
PASSED_TESTS=0
FAILED_TESTS=0
SKIPPED_TESTS=0

# Test results
declare -A TEST_RESULTS

# Initialize test environment
init_test_environment() {
    print_header "Initializing Final Validation Test Environment"
    
    # Create output directories
    mkdir -p "$OUTPUT_DIR"
    mkdir -p "$OUTPUT_DIR/logs"
    mkdir -p "$OUTPUT_DIR/reports"
    mkdir -p "$OUTPUT_DIR/artifacts"
    
    # Set environment variables
    export OSSB_DEBUG="true"
    export OSSB_CACHE_DIR="$OUTPUT_DIR/cache"
    export TEST_OUTPUT_DIR="$OUTPUT_DIR"
    
    # Create cache directory
    mkdir -p "$OSSB_CACHE_DIR"
    
    print_success "Test environment initialized"
    print_info "Output directory: $OUTPUT_DIR"
    print_info "Cache directory: $OSSB_CACHE_DIR"
}

# Check prerequisites
check_prerequisites() {
    print_header "Checking Prerequisites"
    
    local missing_tools=()
    
    # Check Go
    if ! command -v go &> /dev/null; then
        missing_tools+=("go")
    else
        print_success "Go $(go version | cut -d' ' -f3) found"
    fi
    
    # Check OSSB binary
    if [ ! -f "./ossb" ] && ! command -v ossb &> /dev/null; then
        print_error "OSSB binary not found"
        print_info "Please build OSSB first: make build"
        exit 1
    else
        print_success "OSSB binary found"
    fi
    
    # Check container runtime
    if command -v podman &> /dev/null; then
        print_success "Podman found"
        export CONTAINER_RUNTIME="podman"
    elif command -v docker &> /dev/null; then
        print_success "Docker found"
        export CONTAINER_RUNTIME="docker"
    else
        print_warning "No container runtime found (Docker/Podman)"
        print_warning "Some tests will be skipped"
    fi
    
    # Check Kubernetes
    if command -v kubectl &> /dev/null && kubectl cluster-info &> /dev/null; then
        print_success "Kubernetes cluster access available"
        export K8S_AVAILABLE=true
    else
        print_warning "Kubernetes cluster not available"
        print_warning "Kubernetes tests will be skipped"
        export K8S_AVAILABLE=false
    fi
    
    # Check optional tools
    local optional_tools=("jq" "yq" "bc" "curl" "wget")
    for tool in "${optional_tools[@]}"; do
        if command -v "$tool" &> /dev/null; then
            print_success "$tool found"
        else
            print_warning "$tool not found (optional)"
        fi
    done
    
    if [ ${#missing_tools[@]} -gt 0 ]; then
        print_error "Missing required tools: ${missing_tools[*]}"
        exit 1
    fi
    
    print_success "All prerequisites satisfied"
}

# Run test suite
run_test_suite() {
    local suite_name="$1"
    local suite_description="$2"
    local test_command="$3"
    
    print_header "Running Test Suite: $suite_name"
    print_info "Description: $suite_description"
    
    TOTAL_TESTS=$((TOTAL_TESTS + 1))
    
    local start_time=$(date +%s)
    local log_file="$OUTPUT_DIR/logs/${suite_name}.log"
    
    print_info "Executing: $test_command"
    print_info "Log file: $log_file"
    
    if timeout "$TIMEOUT" bash -c "$test_command" > "$log_file" 2>&1; then
        local end_time=$(date +%s)
        local duration=$((end_time - start_time))
        
        print_success "Test suite $suite_name passed (${duration}s)"
        PASSED_TESTS=$((PASSED_TESTS + 1))
        TEST_RESULTS["$suite_name"]="PASSED"
        TEST_RESULTS["${suite_name}_duration"]="$duration"
    else
        local end_time=$(date +%s)
        local duration=$((end_time - start_time))
        
        print_error "Test suite $suite_name failed (${duration}s)"
        FAILED_TESTS=$((FAILED_TESTS + 1))
        TEST_RESULTS["$suite_name"]="FAILED"
        TEST_RESULTS["${suite_name}_duration"]="$duration"
        
        if [ "$VERBOSE" = true ]; then
            print_info "Error details (last 20 lines):"
            tail -20 "$log_file" | sed 's/^/  /'
        fi
    fi
}

# Run real-world Dockerfile tests
run_real_world_tests() {
    local test_cmd="go test -tags=integration -timeout=$TIMEOUT -v ./final_integration_validation_test.go -run TestFinalIntegrationRealWorldDockerfiles"
    run_test_suite "real_world_dockerfiles" "Real-world Dockerfile scenarios" "$test_cmd"
}

# Run OCI compliance validation
run_oci_compliance_tests() {
    local test_cmd="go test -tags=integration -timeout=$TIMEOUT -v ./final_integration_validation_test.go -run TestFinalIntegrationOCICompliance"
    run_test_suite "oci_compliance" "OCI compliance validation" "$test_cmd"
    
    # Run additional OCI validation with script
    if [ -x "./scripts/validate-oci-compliance.sh" ]; then
        local script_cmd="./scripts/validate-oci-compliance.sh --output $OUTPUT_DIR/oci-validation"
        run_test_suite "oci_validation_script" "OCI validation script" "$script_cmd"
    fi
}

# Run Kubernetes integration tests
run_kubernetes_tests() {
    if [ "$K8S_AVAILABLE" != true ]; then
        print_warning "Skipping Kubernetes tests (cluster not available)"
        SKIPPED_TESTS=$((SKIPPED_TESTS + 1))
        TEST_RESULTS["kubernetes_integration"]="SKIPPED"
        return 0
    fi
    
    local test_cmd="go test -tags=integration -timeout=$TIMEOUT -v ./final_integration_validation_test.go -run TestFinalIntegrationKubernetesRealCluster"
    run_test_suite "kubernetes_integration" "Kubernetes integration in real cluster" "$test_cmd"
}

# Run security audit
run_security_audit() {
    local test_cmd="go test -tags=integration -timeout=$TIMEOUT -v ./final_integration_validation_test.go -run TestFinalIntegrationSecurityAudit"
    run_test_suite "security_audit_go" "Security audit (Go tests)" "$test_cmd"
    
    # Run additional security audit with script
    if [ -x "./scripts/security-audit.sh" ]; then
        local script_cmd="./scripts/security-audit.sh --output $OUTPUT_DIR/security-audit"
        run_test_suite "security_audit_script" "Security audit script" "$script_cmd"
    fi
}

# Run performance benchmarks
run_performance_benchmarks() {
    local test_cmd="go test -tags=integration -timeout=$TIMEOUT -v ./final_integration_validation_test.go -run TestFinalIntegrationPerformanceBenchmark"
    run_test_suite "performance_benchmark_go" "Performance benchmark (Go tests)" "$test_cmd"
    
    # Run additional performance benchmarks with script
    if [ -x "./scripts/performance-benchmark.sh" ]; then
        local script_cmd="./scripts/performance-benchmark.sh --output $OUTPUT_DIR/performance-benchmark"
        run_test_suite "performance_benchmark_script" "Performance benchmark script" "$script_cmd"
    fi
}

# Run existing integration tests
run_existing_integration_tests() {
    print_header "Running Existing Integration Test Suite"
    
    local test_cmd="./run_integration_tests.sh --all --output $OUTPUT_DIR/integration-tests"
    run_test_suite "existing_integration_tests" "Existing integration test suite" "$test_cmd"
}

# Generate comprehensive report
generate_comprehensive_report() {
    print_header "Generating Comprehensive Test Report"
    
    local report_file="$OUTPUT_DIR/final-validation-report.md"
    local json_report="$OUTPUT_DIR/final-validation-results.json"
    
    # Generate Markdown report
    cat > "$report_file" << EOF
# OSSB Final Integration and Validation Test Report

Generated on: $(date)
Test Duration: $(date -d @$(($(date +%s) - START_TIME)) -u +%H:%M:%S 2>/dev/null || echo "unknown")
OSSB Version: $(./ossb version 2>/dev/null || echo "unknown")
System: $(uname -s) $(uname -m)

## Executive Summary

This comprehensive test report covers the final integration and validation testing of OSSB,
including real-world scenarios, OCI compliance, Kubernetes integration, security auditing,
and performance benchmarking.

### Test Results Summary

- **Total Test Suites**: $TOTAL_TESTS
- **Passed**: $PASSED_TESTS
- **Failed**: $FAILED_TESTS
- **Skipped**: $SKIPPED_TESTS
- **Success Rate**: $(( (PASSED_TESTS * 100) / (TOTAL_TESTS - SKIPPED_TESTS) ))%

## Detailed Test Results

EOF

    # Add detailed results for each test suite
    for key in "${!TEST_RESULTS[@]}"; do
        if [[ "$key" != *"_duration" ]]; then
            local suite_name="$key"
            local status="${TEST_RESULTS[$key]}"
            local duration="${TEST_RESULTS[${key}_duration]:-unknown}"
            
            echo "### $suite_name" >> "$report_file"
            echo "" >> "$report_file"
            
            case "$status" in
                "PASSED")
                    echo "**Status**: ✅ PASSED" >> "$report_file"
                    ;;
                "FAILED")
                    echo "**Status**: ❌ FAILED" >> "$report_file"
                    ;;
                "SKIPPED")
                    echo "**Status**: ⏭️ SKIPPED" >> "$report_file"
                    ;;
            esac
            
            echo "**Duration**: ${duration}s" >> "$report_file"
            echo "" >> "$report_file"
            
            # Add log excerpt if available
            local log_file="$OUTPUT_DIR/logs/${suite_name}.log"
            if [ -f "$log_file" ]; then
                echo "**Log Excerpt**:" >> "$report_file"
                echo '```' >> "$report_file"
                tail -10 "$log_file" >> "$report_file"
                echo '```' >> "$report_file"
                echo "" >> "$report_file"
            fi
        fi
    done
    
    cat >> "$report_file" << EOF

## Test Environment

- **Operating System**: $(uname -s) $(uname -r)
- **Architecture**: $(uname -m)
- **Container Runtime**: ${CONTAINER_RUNTIME:-none}
- **Kubernetes**: $([ "$K8S_AVAILABLE" = true ] && echo "Available" || echo "Not Available")
- **Go Version**: $(go version 2>/dev/null || echo "unknown")

## Artifacts and Logs

The following artifacts were generated during testing:

- **Test Logs**: \`$OUTPUT_DIR/logs/\`
- **Test Reports**: \`$OUTPUT_DIR/reports/\`
- **Test Artifacts**: \`$OUTPUT_DIR/artifacts/\`
- **Cache Directory**: \`$OSSB_CACHE_DIR\`

## Recommendations

Based on the test results, the following recommendations are provided:

### High Priority
1. Address any failed test suites immediately
2. Review security audit findings
3. Validate OCI compliance issues if any

### Medium Priority
1. Optimize performance based on benchmark results
2. Improve Kubernetes integration if tests were skipped
3. Enhance error handling based on test failures

### Low Priority
1. Consider additional test coverage for edge cases
2. Optimize build cache usage
3. Improve documentation based on test findings

## Next Steps

1. **Review Failed Tests**: Investigate and fix any failed test suites
2. **Security Review**: Address any security vulnerabilities found
3. **Performance Optimization**: Implement performance improvements based on benchmarks
4. **Documentation Update**: Update documentation based on test findings
5. **Release Preparation**: Prepare for release once all critical issues are resolved

---

*This report was generated automatically by the OSSB Final Validation Test Suite.*
*For detailed logs and artifacts, see the test output directory: \`$OUTPUT_DIR\`*

EOF

    # Generate JSON report
    cat > "$json_report" << EOF
{
  "timestamp": "$(date -u +%Y-%m-%dT%H:%M:%SZ)",
  "ossb_version": "$(./ossb version 2>/dev/null || echo "unknown")",
  "system": {
    "os": "$(uname -s)",
    "arch": "$(uname -m)",
    "kernel": "$(uname -r)"
  },
  "test_summary": {
    "total": $TOTAL_TESTS,
    "passed": $PASSED_TESTS,
    "failed": $FAILED_TESTS,
    "skipped": $SKIPPED_TESTS,
    "success_rate": $(( (PASSED_TESTS * 100) / (TOTAL_TESTS - SKIPPED_TESTS) ))
  },
  "test_results": {
EOF

    # Add test results to JSON
    local first=true
    for key in "${!TEST_RESULTS[@]}"; do
        if [[ "$key" != *"_duration" ]]; then
            if [ "$first" = false ]; then
                echo "," >> "$json_report"
            fi
            first=false
            
            local suite_name="$key"
            local status="${TEST_RESULTS[$key]}"
            local duration="${TEST_RESULTS[${key}_duration]:-0}"
            
            cat >> "$json_report" << EOF
    "$suite_name": {
      "status": "$status",
      "duration": $duration
    }EOF
        fi
    done
    
    cat >> "$json_report" << EOF

  },
  "environment": {
    "container_runtime": "${CONTAINER_RUNTIME:-none}",
    "kubernetes_available": $([ "$K8S_AVAILABLE" = true ] && echo "true" || echo "false"),
    "go_version": "$(go version 2>/dev/null | cut -d' ' -f3 || echo "unknown")"
  },
  "artifacts": {
    "output_directory": "$OUTPUT_DIR",
    "logs_directory": "$OUTPUT_DIR/logs",
    "reports_directory": "$OUTPUT_DIR/reports",
    "cache_directory": "$OSSB_CACHE_DIR"
  }
}
EOF

    print_success "Comprehensive test report generated:"
    print_info "  Markdown: $report_file"
    print_info "  JSON: $json_report"
}

# Clean up test environment
cleanup_test_environment() {
    print_header "Cleaning Up Test Environment"
    
    # Clean up containers
    if [ "$CONTAINER_RUNTIME" = "docker" ]; then
        docker system prune -f --filter "label=ossb-test" &> /dev/null || true
    elif [ "$CONTAINER_RUNTIME" = "podman" ]; then
        podman system prune -f &> /dev/null || true
    fi
    
    # Clean up Kubernetes resources if available
    if [ "$K8S_AVAILABLE" = true ]; then
        kubectl delete jobs,configmaps,secrets -l app=ossb-test &> /dev/null || true
    fi
    
    print_success "Test environment cleaned up"
}

# Show usage information
show_usage() {
    cat << EOF
OSSB Final Integration and Validation Test Runner

Usage: $0 [OPTIONS]

Options:
    -h, --help              Show this help message
    -c, --config FILE       Test configuration file (default: test_config_final.yaml)
    -o, --output DIR        Output directory (default: ./test-results/final-validation)
    -t, --timeout DURATION  Test timeout (default: 45m)
    -v, --verbose           Enable verbose output
    --quick                 Run quick validation tests only
    --full                  Run comprehensive validation tests (default)
    --real-world            Run real-world Dockerfile tests only
    --oci                   Run OCI compliance tests only
    --kubernetes            Run Kubernetes integration tests only
    --security              Run security audit only
    --performance           Run performance benchmarks only
    --existing              Run existing integration tests only
    --no-cleanup            Skip cleanup after tests

Examples:
    # Run full validation suite
    $0

    # Run with custom output directory
    $0 --output /tmp/validation-results

    # Run only security audit
    $0 --security

    # Run quick validation tests
    $0 --quick

    # Run with verbose output
    $0 --verbose

EOF
}

# Main execution function
main() {
    local run_real_world=true
    local run_oci=true
    local run_kubernetes=true
    local run_security=true
    local run_performance=true
    local run_existing=true
    local quick_mode=false
    local no_cleanup=false
    
    # Parse command line arguments
    while [[ $# -gt 0 ]]; do
        case $1 in
            -h|--help)
                show_usage
                exit 0
                ;;
            -c|--config)
                CONFIG_FILE="$2"
                shift 2
                ;;
            -o|--output)
                OUTPUT_DIR="$2"
                shift 2
                ;;
            -t|--timeout)
                TIMEOUT="$2"
                shift 2
                ;;
            -v|--verbose)
                VERBOSE=true
                shift
                ;;
            --quick)
                quick_mode=true
                TIMEOUT="15m"
                shift
                ;;
            --full)
                quick_mode=false
                shift
                ;;
            --real-world)
                run_real_world=true
                run_oci=false
                run_kubernetes=false
                run_security=false
                run_performance=false
                run_existing=false
                shift
                ;;
            --oci)
                run_real_world=false
                run_oci=true
                run_kubernetes=false
                run_security=false
                run_performance=false
                run_existing=false
                shift
                ;;
            --kubernetes)
                run_real_world=false
                run_oci=false
                run_kubernetes=true
                run_security=false
                run_performance=false
                run_existing=false
                shift
                ;;
            --security)
                run_real_world=false
                run_oci=false
                run_kubernetes=false
                run_security=true
                run_performance=false
                run_existing=false
                shift
                ;;
            --performance)
                run_real_world=false
                run_oci=false
                run_kubernetes=false
                run_security=false
                run_performance=true
                run_existing=false
                shift
                ;;
            --existing)
                run_real_world=false
                run_oci=false
                run_kubernetes=false
                run_security=false
                run_performance=false
                run_existing=true
                shift
                ;;
            --no-cleanup)
                no_cleanup=true
                shift
                ;;
            -*)
                print_error "Unknown option: $1"
                show_usage
                exit 1
                ;;
            *)
                print_error "Unexpected argument: $1"
                show_usage
                exit 1
                ;;
        esac
    done
    
    # Record start time
    START_TIME=$(date +%s)
    
    # Set up cleanup trap
    if [ "$no_cleanup" != true ]; then
        trap cleanup_test_environment EXIT
    fi
    
    print_header "OSSB Final Integration and Validation Test Suite"
    print_info "Configuration: $CONFIG_FILE"
    print_info "Output directory: $OUTPUT_DIR"
    print_info "Timeout: $TIMEOUT"
    print_info "Quick mode: $quick_mode"
    
    # Initialize environment
    init_test_environment
    check_prerequisites
    
    # Run test suites based on configuration
    if [ "$run_existing" = true ]; then
        run_existing_integration_tests
    fi
    
    if [ "$run_real_world" = true ]; then
        run_real_world_tests
    fi
    
    if [ "$run_oci" = true ]; then
        run_oci_compliance_tests
    fi
    
    if [ "$run_kubernetes" = true ]; then
        run_kubernetes_tests
    fi
    
    if [ "$run_security" = true ]; then
        run_security_audit
    fi
    
    if [ "$run_performance" = true ] && [ "$quick_mode" != true ]; then
        run_performance_benchmarks
    fi
    
    # Generate comprehensive report
    generate_comprehensive_report
    
    # Print final summary
    print_header "Final Validation Test Summary"
    print_info "Total test suites: $TOTAL_TESTS"
    print_info "Passed: $PASSED_TESTS"
    print_info "Failed: $FAILED_TESTS"
    print_info "Skipped: $SKIPPED_TESTS"
    
    if [ $FAILED_TESTS -eq 0 ]; then
        print_success "All validation tests completed successfully!"
        print_info "OSSB is ready for production use."
        exit 0
    else
        print_error "$FAILED_TESTS validation test(s) failed"
        print_info "Please review the test results and address any issues."
        print_info "Detailed results: $OUTPUT_DIR/final-validation-report.md"
        exit 1
    fi
}

# Run main function
main "$@"