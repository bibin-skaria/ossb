package registry

import (
	"testing"
)

func TestExampleImageReferenceValidation(t *testing.T) {
	// This test just ensures the example function runs without panicking
	ExampleImageReferenceValidation()
}

func TestExamplePlatformCompatibility(t *testing.T) {
	// This test just ensures the example function runs without panicking
	ExamplePlatformCompatibility()
}

func TestExamplePullAndExtractImage(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	
	// This test ensures the pull and extract example works
	ExamplePullAndExtractImage()
}

func TestExampleMultiArchImagePull(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	
	// This test ensures the multi-arch pull example works
	ExampleMultiArchImagePull()
}