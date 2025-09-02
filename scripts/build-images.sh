#!/bin/bash
# OSSB Docker Images Build Script

set -e

# Configuration
REGISTRY=${REGISTRY:-"bibin9992009"}
OSSB_VERSION=${OSSB_VERSION:-"main"}
OSSB_REPO=${OSSB_REPO:-"https://github.com/bibin-skaria/ossb.git"}

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

print_header() {
    echo -e "${BLUE}======================================${NC}"
    echo -e "${BLUE}$1${NC}"
    echo -e "${BLUE}======================================${NC}"
}

print_success() {
    echo -e "${GREEN}✅ $1${NC}"
}

print_warning() {
    echo -e "${YELLOW}⚠️  $1${NC}"
}

print_error() {
    echo -e "${RED}❌ $1${NC}"
}

usage() {
    echo "Usage: $0 [OPTIONS] [COMMAND]"
    echo ""
    echo "Commands:"
    echo "  ossb                 Build OSSB (Kaniko alternative) image"
    echo "  jenkins-agent        Build Jenkins agent with OSSB for Fargate"
    echo "  all                  Build all images"
    echo "  push                 Push all built images to registry"
    echo ""
    echo "Options:"
    echo "  -r, --registry       Container registry (default: ghcr.io/bibin-skaria)"
    echo "  -v, --version        OSSB version/branch to build (default: main)"
    echo "  -u, --repo-url       OSSB repository URL"
    echo "  -t, --tag            Additional tag for images"
    echo "  --no-cache           Build without Docker cache"
    echo "  --platform           Target platform (default: linux/amd64)"
    echo "  -h, --help           Show this help message"
    echo ""
    echo "Examples:"
    echo "  $0 ossb                                    # Build OSSB image"
    echo "  $0 jenkins-agent --tag latest              # Build Jenkins agent"
    echo "  $0 all --registry myregistry.com          # Build all with custom registry"
    echo "  $0 push                                    # Push all images"
    echo "  $0 ossb --version v1.0.0 --no-cache       # Build specific version without cache"
}

build_ossb() {
    print_header "Building OSSB (Kaniko Alternative) Image"
    
    local tag="${REGISTRY}/ossb-builder:${OSSB_VERSION}"
    local additional_tags=""
    
    if [ -n "$ADDITIONAL_TAG" ]; then
        additional_tags="-t ${REGISTRY}/ossb-builder:${ADDITIONAL_TAG}"
    fi
    
    echo "Building OSSB image with:"
    echo "  Repository: $OSSB_REPO"
    echo "  Version: $OSSB_VERSION"
    echo "  Tag: $tag"
    echo "  Platform: $PLATFORM"
    
    docker build $DOCKER_BUILD_ARGS \
        --platform $PLATFORM \
        --build-arg OSSB_VERSION="$OSSB_VERSION" \
        --build-arg OSSB_REPO="$OSSB_REPO" \
        -t "$tag" \
        $additional_tags \
        -f Dockerfile.ossb .
    
    print_success "OSSB image built: $tag"
    
    # Test the built image
    echo "Testing OSSB image..."
    if docker run --rm "$tag" --help >/dev/null 2>&1; then
        print_success "OSSB image test passed"
    else
        print_error "OSSB image test failed"
        return 1
    fi
}

build_jenkins_agent() {
    print_header "Building Jenkins Agent with OSSB for Fargate"
    
    local tag="${REGISTRY}/jenkins-agent-ossb:${OSSB_VERSION}"
    local additional_tags=""
    
    if [ -n "$ADDITIONAL_TAG" ]; then
        additional_tags="-t ${REGISTRY}/jenkins-agent-ossb:${ADDITIONAL_TAG}"
    fi
    
    echo "Building Jenkins Agent image with:"
    echo "  Repository: $OSSB_REPO"  
    echo "  Version: $OSSB_VERSION"
    echo "  Tag: $tag"
    echo "  Platform: $PLATFORM"
    
    docker build $DOCKER_BUILD_ARGS \
        --platform $PLATFORM \
        --build-arg OSSB_VERSION="$OSSB_VERSION" \
        --build-arg OSSB_REPO="$OSSB_REPO" \
        -t "$tag" \
        $additional_tags \
        -f docker/jenkins-agent-fargate.Dockerfile .
    
    print_success "Jenkins Agent image built: $tag"
    
    # Test the built image
    echo "Testing Jenkins Agent image..."
    if docker run --rm --entrypoint="" "$tag" ossb --version >/dev/null 2>&1; then
        print_success "Jenkins Agent image test passed"
    else
        print_error "Jenkins Agent image test failed"
        return 1
    fi
}

push_images() {
    print_header "Pushing Images to Registry"
    
    local images=(
        "${REGISTRY}/ossb-builder:${OSSB_VERSION}"
        "${REGISTRY}/jenkins-agent-ossb:${OSSB_VERSION}"
    )
    
    if [ -n "$ADDITIONAL_TAG" ]; then
        images+=(
            "${REGISTRY}/ossb-builder:${ADDITIONAL_TAG}"
            "${REGISTRY}/jenkins-agent-ossb:${ADDITIONAL_TAG}"
        )
    fi
    
    for image in "${images[@]}"; do
        if docker image inspect "$image" >/dev/null 2>&1; then
            echo "Pushing $image..."
            if docker push "$image"; then
                print_success "Pushed: $image"
            else
                print_error "Failed to push: $image"
            fi
        else
            print_warning "Image not found: $image (skipping)"
        fi
    done
}

# Parse command line arguments
DOCKER_BUILD_ARGS=""
PLATFORM="linux/amd64"
ADDITIONAL_TAG=""

while [[ $# -gt 0 ]]; do
    case $1 in
        -r|--registry)
            REGISTRY="$2"
            shift 2
            ;;
        -v|--version)
            OSSB_VERSION="$2"
            shift 2
            ;;
        -u|--repo-url)
            OSSB_REPO="$2"
            shift 2
            ;;
        -t|--tag)
            ADDITIONAL_TAG="$2"
            shift 2
            ;;
        --no-cache)
            DOCKER_BUILD_ARGS="$DOCKER_BUILD_ARGS --no-cache"
            shift
            ;;
        --platform)
            PLATFORM="$2"
            shift 2
            ;;
        -h|--help)
            usage
            exit 0
            ;;
        ossb|jenkins-agent|all|push)
            COMMAND="$1"
            shift
            ;;
        *)
            print_error "Unknown option: $1"
            usage
            exit 1
            ;;
    esac
done

# Main execution
case "${COMMAND:-all}" in
    ossb)
        build_ossb
        ;;
    jenkins-agent)
        build_jenkins_agent
        ;;
    all)
        build_ossb
        build_jenkins_agent
        ;;
    push)
        push_images
        ;;
    *)
        print_error "Unknown command: $COMMAND"
        usage
        exit 1
        ;;
esac

print_success "Build script completed successfully!"

echo ""
echo "📋 Next Steps:"
echo ""
echo "1. Test the images:"
echo "   docker run --rm ${REGISTRY}/ossb-builder:${OSSB_VERSION} --help"
echo ""
echo "2. Push to registry:"
echo "   $0 push"
echo ""
echo "3. Use in Kubernetes:"
echo "   See docs/KANIKO-vs-OSSB.md for migration examples"
echo ""
echo "4. Use in Jenkins:"
echo "   Deploy jenkins-agent-ossb to ECS Fargate using docker/ECS-Fargate-Deployment-Guide.md"