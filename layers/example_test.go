package layers_test

import (
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bibin-skaria/ossb/layers"
)

// TestLayerManagerUsage demonstrates basic usage of the layer management system
func TestLayerManagerUsage(t *testing.T) {
	// Create a layer manager with gzip compression
	config := layers.LayerConfig{
		Compression: layers.CompressionGzip,
		SkipEmpty:   true,
	}
	
	lm := layers.NewLayerManager(config)
	
	// Create some file changes representing a typical container layer
	changes := []layers.FileChange{
		{
			Path:      "/app/main",
			Type:      layers.ChangeTypeAdd,
			Mode:      0755,
			Size:      11,
			Content:   strings.NewReader("#!/bin/bash"),
			Timestamp: time.Now(),
		},
		{
			Path:      "/etc/config.json",
			Type:      layers.ChangeTypeAdd,
			Mode:      0644,
			Size:      16, // `{"debug": false}` is 16 bytes
			Content:   strings.NewReader(`{"debug": false}`),
			Timestamp: time.Now(),
		},
		{
			Path:      "/var/log",
			Type:      layers.ChangeTypeAdd,
			Mode:      os.ModeDir | 0755,
			Size:      0,
			Timestamp: time.Now(),
		},
	}
	
	// Create the layer
	layer, err := lm.CreateLayer(changes)
	if err != nil {
		log.Fatalf("Failed to create layer: %v", err)
	}
	
	// Validate the layer
	if err := lm.ValidateLayer(layer); err != nil {
		log.Fatalf("Layer validation failed: %v", err)
	}
	
	// Verify the layer was created successfully
	if layer.Digest == "" {
		t.Error("Expected layer digest to be set")
	}
	
	if layer.Size <= 0 {
		t.Error("Expected layer size to be positive")
	}
	
	if layer.MediaType != layers.MediaTypeImageLayerGzip {
		t.Errorf("Expected media type %s, got %s", layers.MediaTypeImageLayerGzip, layer.MediaType)
	}
	
	t.Logf("Created layer with digest: %s", layer.Digest[:12]+"...")
	t.Logf("Layer size: %d bytes", layer.Size)
	t.Logf("Media type: %s", layer.MediaType)
}

// TestLayerManagerDetectChanges demonstrates filesystem change detection
func TestLayerManagerDetectChanges(t *testing.T) {
	// Create temporary directories
	tempDir, err := os.MkdirTemp("", "layer_example")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(tempDir)
	
	oldDir := filepath.Join(tempDir, "old")
	newDir := filepath.Join(tempDir, "new")
	
	// Create old state
	os.MkdirAll(oldDir, 0755)
	os.WriteFile(filepath.Join(oldDir, "file1.txt"), []byte("original"), 0644)
	os.WriteFile(filepath.Join(oldDir, "file2.txt"), []byte("delete me"), 0644)
	
	// Create new state
	os.MkdirAll(newDir, 0755)
	os.WriteFile(filepath.Join(newDir, "file1.txt"), []byte("modified"), 0644)
	os.WriteFile(filepath.Join(newDir, "file3.txt"), []byte("new file"), 0644)
	// file2.txt is deleted (not present in new)
	
	config := layers.LayerConfig{
		Compression: layers.CompressionGzip,
	}
	lm := layers.NewLayerManager(config)
	
	// Detect changes
	changes, err := lm.DetectChanges(oldDir, newDir)
	if err != nil {
		log.Fatalf("Failed to detect changes: %v", err)
	}
	
	// Group changes by type
	groups := layers.GroupChangesByType(changes)
	
	// Verify the changes were detected correctly
	if len(changes) == 0 {
		t.Error("Expected to detect changes")
	}
	
	if len(groups[layers.ChangeTypeAdd]) == 0 {
		t.Error("Expected to detect additions")
	}
	
	if len(groups[layers.ChangeTypeModify]) == 0 {
		t.Error("Expected to detect modifications")
	}
	
	if len(groups[layers.ChangeTypeDelete]) == 0 {
		t.Error("Expected to detect deletions")
	}
	
	t.Logf("Detected %d changes:", len(changes))
	t.Logf("- %d additions", len(groups[layers.ChangeTypeAdd]))
	t.Logf("- %d modifications", len(groups[layers.ChangeTypeModify]))
	t.Logf("- %d deletions", len(groups[layers.ChangeTypeDelete]))
}

// TestLayerManagerExtraction demonstrates layer extraction
func TestLayerManagerExtraction(t *testing.T) {
	config := layers.LayerConfig{
		Compression: layers.CompressionNone, // No compression for simplicity
	}
	lm := layers.NewLayerManager(config)
	
	// Create a layer with some content
	changes := []layers.FileChange{
		{
			Path:      "/extracted.txt",
			Type:      layers.ChangeTypeAdd,
			Mode:      0644,
			Size:      12,
			Content:   strings.NewReader("Hello World!"),
			Timestamp: time.Now(),
		},
	}
	
	layer, err := lm.CreateLayer(changes)
	if err != nil {
		log.Fatalf("Failed to create layer: %v", err)
	}
	
	// Create temporary directory for extraction
	tempDir, err := os.MkdirTemp("", "extract_example")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(tempDir)
	
	// Extract the layer
	if err := lm.ExtractLayer(layer, tempDir); err != nil {
		log.Fatalf("Failed to extract layer: %v", err)
	}
	
	// Verify extraction
	extractedFile := filepath.Join(tempDir, "extracted.txt")
	content, err := os.ReadFile(extractedFile)
	if err != nil {
		log.Fatalf("Failed to read extracted file: %v", err)
	}
	
	// Verify extraction
	expectedContent := "Hello World!"
	if string(content) != expectedContent {
		t.Errorf("Expected content '%s', got '%s'", expectedContent, string(content))
	}
	
	t.Logf("Extracted file content: %s", string(content))
}

// TestLayerManagerDeduplication demonstrates layer deduplication
func TestLayerManagerDeduplication(t *testing.T) {
	config := layers.LayerConfig{
		Compression: layers.CompressionNone,
	}
	lm := layers.NewLayerManager(config)
	
	// Create identical changes (will result in identical layers)
	changes := []layers.FileChange{
		{
			Path:      "/same.txt",
			Type:      layers.ChangeTypeAdd,
			Mode:      0644,
			Size:      4,
			Content:   strings.NewReader("same"),
			Timestamp: time.Unix(1234567890, 0), // Fixed timestamp
		},
	}
	
	// Create multiple layers with the same content
	layer1, _ := lm.CreateLayer(changes)
	
	// Reset content reader for second layer
	changes[0].Content = strings.NewReader("same")
	layer2, _ := lm.CreateLayer(changes)
	
	// Reset content reader for third layer
	changes[0].Content = strings.NewReader("same")
	layer3, _ := lm.CreateLayer(changes)
	
	layers := []*layers.Layer{layer1, layer2, layer3}
	
	// Deduplicate layers
	deduplicated, err := lm.DeduplicateLayers(layers)
	if err != nil {
		log.Fatalf("Failed to deduplicate layers: %v", err)
	}
	
	// Verify deduplication worked
	if len(deduplicated) != 1 {
		t.Errorf("Expected 1 deduplicated layer, got %d", len(deduplicated))
	}
	
	allSameDigest := layer1.Digest == layer2.Digest && layer2.Digest == layer3.Digest
	if !allSameDigest {
		t.Error("Expected all layers to have the same digest")
	}
	
	t.Logf("Original layers: %d", len(layers))
	t.Logf("Deduplicated layers: %d", len(deduplicated))
	t.Logf("All layers have same digest: %t", allSameDigest)
}