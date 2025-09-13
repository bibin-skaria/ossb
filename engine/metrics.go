package engine

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"time"

	"github.com/bibin-skaria/ossb/internal/types"
)

// MetricsCollector collects build metrics including timing, cache hits, and resource usage
type MetricsCollector struct {
	mutex           sync.RWMutex
	buildID         string
	startTime       time.Time
	endTime         *time.Time
	stages          map[string]*StageMetrics
	operations      map[string]*OperationMetrics
	resourceSamples []ResourceSample
	cacheMetrics    *BuildCacheMetrics
	registryMetrics *RegistryMetrics
	platforms       []types.Platform
	samplingTicker  *time.Ticker
	stopSampling    chan bool
}

// StageMetrics represents metrics for a build stage
type StageMetrics struct {
	Name              string                     `json:"name"`
	Platform          string                     `json:"platform,omitempty"`
	StartTime         time.Time                  `json:"start_time"`
	EndTime           *time.Time                 `json:"end_time,omitempty"`
	Duration          time.Duration              `json:"duration"`
	Operations        int                        `json:"operations"`
	CacheHits         int                        `json:"cache_hits"`
	CacheMisses       int                        `json:"cache_misses"`
	BytesProcessed    int64                      `json:"bytes_processed"`
	LayersCreated     int                        `json:"layers_created"`
	LayersReused      int                        `json:"layers_reused"`
	RegistryOps       int                        `json:"registry_operations"`
	Errors            int                        `json:"errors"`
	PeakMemoryMB      int64                      `json:"peak_memory_mb"`
	AvgCPUPercent     float64                    `json:"avg_cpu_percent"`
	DiskIOBytes       int64                      `json:"disk_io_bytes"`
	NetworkIOBytes    int64                      `json:"network_io_bytes"`
	CustomMetrics     map[string]interface{}     `json:"custom_metrics,omitempty"`
}

// OperationMetrics represents metrics for individual operations
type OperationMetrics struct {
	Type           types.OperationType `json:"type"`
	Platform       string              `json:"platform,omitempty"`
	StartTime      time.Time           `json:"start_time"`
	Duration       time.Duration       `json:"duration"`
	CacheHit       bool                `json:"cache_hit"`
	BytesIn        int64               `json:"bytes_in"`
	BytesOut       int64               `json:"bytes_out"`
	MemoryUsageMB  int64               `json:"memory_usage_mb"`
	CPUPercent     float64             `json:"cpu_percent"`
	Success        bool                `json:"success"`
	Error          string              `json:"error,omitempty"`
	Metadata       map[string]interface{} `json:"metadata,omitempty"`
}

// ResourceSample represents a point-in-time resource usage sample
type ResourceSample struct {
	Timestamp     time.Time `json:"timestamp"`
	MemoryMB      int64     `json:"memory_mb"`
	CPUPercent    float64   `json:"cpu_percent"`
	DiskUsageMB   int64     `json:"disk_usage_mb"`
	NetworkRxMB   int64     `json:"network_rx_mb"`
	NetworkTxMB   int64     `json:"network_tx_mb"`
	GoroutineCount int      `json:"goroutine_count"`
	GCPauseMS     float64   `json:"gc_pause_ms"`
}

// BuildCacheMetrics represents cache-related metrics for builds (extends CacheMetrics)
type BuildCacheMetrics struct {
	*CacheMetrics
	AvgLookupTimeMS float64 `json:"avg_lookup_time_ms"`
	AvgStoreTimeMS  float64 `json:"avg_store_time_ms"`
}

// RegistryMetrics represents registry operation metrics
type RegistryMetrics struct {
	PullOperations  int           `json:"pull_operations"`
	PushOperations  int           `json:"push_operations"`
	TotalPullTime   time.Duration `json:"total_pull_time"`
	TotalPushTime   time.Duration `json:"total_push_time"`
	BytesDownloaded int64         `json:"bytes_downloaded"`
	BytesUploaded   int64         `json:"bytes_uploaded"`
	AuthFailures    int           `json:"auth_failures"`
	NetworkErrors   int           `json:"network_errors"`
	Retries         int           `json:"retries"`
	AvgPullSpeedMBs float64       `json:"avg_pull_speed_mbs"`
	AvgPushSpeedMBs float64       `json:"avg_push_speed_mbs"`
}

// BuildMetrics represents comprehensive build metrics
type BuildMetrics struct {
	BuildID          string                        `json:"build_id"`
	StartTime        time.Time                     `json:"start_time"`
	EndTime          *time.Time                    `json:"end_time,omitempty"`
	Duration         time.Duration                 `json:"duration"`
	Success          bool                          `json:"success"`
	Platforms        []string                      `json:"platforms"`
	TotalOperations  int                           `json:"total_operations"`
	TotalCacheHits   int                           `json:"total_cache_hits"`
	TotalCacheMisses int                           `json:"total_cache_misses"`
	CacheHitRate     float64                       `json:"cache_hit_rate"`
	TotalBytesProcessed int64                      `json:"total_bytes_processed"`
	PeakMemoryMB     int64                         `json:"peak_memory_mb"`
	AvgMemoryMB      int64                         `json:"avg_memory_mb"`
	PeakCPUPercent   float64                       `json:"peak_cpu_percent"`
	AvgCPUPercent    float64                       `json:"avg_cpu_percent"`
	TotalDiskIOBytes int64                         `json:"total_disk_io_bytes"`
	TotalNetworkIOBytes int64                      `json:"total_network_io_bytes"`
	StageMetrics     map[string]*StageMetrics      `json:"stage_metrics"`
	OperationMetrics []*OperationMetrics           `json:"operation_metrics"`
	ResourceSamples  []ResourceSample              `json:"resource_samples"`
	CacheMetrics     *BuildCacheMetrics            `json:"cache_metrics"`
	RegistryMetrics  *RegistryMetrics              `json:"registry_metrics"`
	ErrorCount       int                           `json:"error_count"`
	WarningCount     int                           `json:"warning_count"`
	CustomMetrics    map[string]interface{}        `json:"custom_metrics,omitempty"`
}

// NewMetricsCollector creates a new metrics collector
func NewMetricsCollector(buildID string, platforms []types.Platform) *MetricsCollector {
	collector := &MetricsCollector{
		buildID:         buildID,
		startTime:       time.Now(),
		stages:          make(map[string]*StageMetrics),
		operations:      make(map[string]*OperationMetrics),
		resourceSamples: make([]ResourceSample, 0),
		platforms:       platforms,
		stopSampling:    make(chan bool, 1),
		cacheMetrics: &BuildCacheMetrics{
			CacheMetrics: &CacheMetrics{
				PlatformStats: make(map[string]*PlatformCacheStats),
			},
		},
		registryMetrics: &RegistryMetrics{},
	}

	// Start resource sampling
	collector.startResourceSampling()

	return collector
}

// StartStage starts collecting metrics for a stage
func (m *MetricsCollector) StartStage(stageName string, platform string) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	stageKey := m.getStageKey(stageName, platform)
	
	stage := &StageMetrics{
		Name:          stageName,
		Platform:      platform,
		StartTime:     time.Now(),
		CustomMetrics: make(map[string]interface{}),
	}

	m.stages[stageKey] = stage
}

// EndStage ends metric collection for a stage
func (m *MetricsCollector) EndStage(stageName string, platform string, success bool) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	stageKey := m.getStageKey(stageName, platform)
	stage, exists := m.stages[stageKey]
	if !exists {
		return
	}

	endTime := time.Now()
	stage.EndTime = &endTime
	stage.Duration = endTime.Sub(stage.StartTime)

	// Calculate average CPU usage for the stage
	if len(m.resourceSamples) > 0 {
		totalCPU := 0.0
		samples := 0
		for _, sample := range m.resourceSamples {
			if sample.Timestamp.After(stage.StartTime) && (stage.EndTime == nil || sample.Timestamp.Before(*stage.EndTime)) {
				totalCPU += sample.CPUPercent
				samples++
				if sample.MemoryMB > stage.PeakMemoryMB {
					stage.PeakMemoryMB = sample.MemoryMB
				}
			}
		}
		if samples > 0 {
			stage.AvgCPUPercent = totalCPU / float64(samples)
		}
	}
}

// RecordOperation records metrics for an operation
func (m *MetricsCollector) RecordOperation(ctx context.Context, operation *types.Operation, result *types.OperationResult, duration time.Duration) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	opKey := fmt.Sprintf("%s-%d", operation.Type, time.Now().UnixNano())
	
	opMetrics := &OperationMetrics{
		Type:      operation.Type,
		Platform:  operation.Platform.String(),
		StartTime: time.Now().Add(-duration),
		Duration:  duration,
		CacheHit:  result.CacheHit,
		Success:   result.Success,
		Error:     result.Error,
		Metadata:  make(map[string]interface{}),
	}

	// Get current memory usage
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)
	opMetrics.MemoryUsageMB = int64(memStats.Alloc / 1024 / 1024)

	// Add operation-specific metadata
	if operation.Type == types.OperationTypePull || operation.Type == types.OperationTypePush {
		if len(operation.Inputs) > 0 {
			opMetrics.Metadata["image"] = operation.Inputs[0]
		}
	}

	m.operations[opKey] = opMetrics

	// Update stage metrics - find the most recent active stage for this platform
	var targetStage *StageMetrics
	for _, stage := range m.stages {
		if (stage.Platform == operation.Platform.String() || stage.Platform == "") && stage.EndTime == nil {
			if targetStage == nil || stage.StartTime.After(targetStage.StartTime) {
				targetStage = stage
			}
		}
	}
	
	if targetStage != nil {
		targetStage.Operations++
		if result.CacheHit {
			targetStage.CacheHits++
		} else {
			targetStage.CacheMisses++
		}
		if !result.Success {
			targetStage.Errors++
		}
	}
}

// RecordCacheMetrics records cache-related metrics
func (m *MetricsCollector) RecordCacheMetrics(cacheMetrics *types.CacheMetrics) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	m.cacheMetrics.CacheMetrics = &CacheMetrics{
		TotalHits:         cacheMetrics.TotalHits,
		TotalMisses:       cacheMetrics.TotalMisses,
		HitRate:           cacheMetrics.HitRate,
		TotalSize:         cacheMetrics.TotalSize,
		TotalFiles:        cacheMetrics.TotalFiles,
		InvalidationCount: cacheMetrics.InvalidationCount,
		PruningCount:      cacheMetrics.PruningCount,
		SharedEntries:     cacheMetrics.SharedEntries,
		PlatformStats:     make(map[string]*PlatformCacheStats),
	}

	// Convert platform stats
	for platform, stats := range cacheMetrics.PlatformStats {
		m.cacheMetrics.PlatformStats[platform] = &PlatformCacheStats{
			Hits:        stats.Hits,
			Misses:      stats.Misses,
			TotalSize:   stats.TotalSize,
			TotalFiles:  stats.TotalFiles,
			LastUpdated: stats.LastUpdated,
		}
	}
}

// RecordRegistryOperation records a registry operation
func (m *MetricsCollector) RecordRegistryOperation(operation string, duration time.Duration, bytesTransferred int64, success bool) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	switch operation {
	case "pull":
		m.registryMetrics.PullOperations++
		m.registryMetrics.TotalPullTime += duration
		m.registryMetrics.BytesDownloaded += bytesTransferred
		if duration > 0 {
			speedMBs := float64(bytesTransferred) / 1024 / 1024 / duration.Seconds()
			if m.registryMetrics.PullOperations == 1 {
				m.registryMetrics.AvgPullSpeedMBs = speedMBs
			} else {
				m.registryMetrics.AvgPullSpeedMBs = (m.registryMetrics.AvgPullSpeedMBs + speedMBs) / 2
			}
		}
	case "push":
		m.registryMetrics.PushOperations++
		m.registryMetrics.TotalPushTime += duration
		m.registryMetrics.BytesUploaded += bytesTransferred
		if duration > 0 {
			speedMBs := float64(bytesTransferred) / 1024 / 1024 / duration.Seconds()
			if m.registryMetrics.PushOperations == 1 {
				m.registryMetrics.AvgPushSpeedMBs = speedMBs
			} else {
				m.registryMetrics.AvgPushSpeedMBs = (m.registryMetrics.AvgPushSpeedMBs + speedMBs) / 2
			}
		}
	}

	if !success {
		m.registryMetrics.NetworkErrors++
	}
}

// RecordError records an error occurrence
func (m *MetricsCollector) RecordError(errorType string, message string) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	// This could be extended to categorize errors
	// For now, we just increment the error count
}

// AddCustomMetric adds a custom metric
func (m *MetricsCollector) AddCustomMetric(key string, value interface{}) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	// Add to the most recent stage if available
	for _, stage := range m.stages {
		if stage.EndTime == nil {
			stage.CustomMetrics[key] = value
			break
		}
	}
}

// GetMetrics returns comprehensive build metrics
func (m *MetricsCollector) GetMetrics() *BuildMetrics {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	metrics := &BuildMetrics{
		BuildID:         m.buildID,
		StartTime:       m.startTime,
		EndTime:         m.endTime,
		StageMetrics:    make(map[string]*StageMetrics),
		OperationMetrics: make([]*OperationMetrics, 0, len(m.operations)),
		ResourceSamples: make([]ResourceSample, len(m.resourceSamples)),
		CacheMetrics:    m.cacheMetrics,
		RegistryMetrics: m.registryMetrics,
		CustomMetrics:   make(map[string]interface{}),
	}

	if m.endTime != nil {
		metrics.Duration = m.endTime.Sub(m.startTime)
		metrics.Success = true // If we have an end time, assume success unless explicitly set
	} else {
		metrics.Duration = time.Since(m.startTime)
		metrics.Success = false
	}

	// Convert platforms to strings
	metrics.Platforms = make([]string, len(m.platforms))
	for i, platform := range m.platforms {
		metrics.Platforms[i] = platform.String()
	}

	// Copy stage metrics
	totalOps := 0
	totalCacheHits := 0
	totalCacheMisses := 0
	totalBytesProcessed := int64(0)
	errorCount := 0

	for key, stage := range m.stages {
		stageCopy := *stage
		metrics.StageMetrics[key] = &stageCopy
		
		totalOps += stage.Operations
		totalCacheHits += stage.CacheHits
		totalCacheMisses += stage.CacheMisses
		totalBytesProcessed += stage.BytesProcessed
		errorCount += stage.Errors
	}

	metrics.TotalOperations = totalOps
	metrics.TotalCacheHits = totalCacheHits
	metrics.TotalCacheMisses = totalCacheMisses
	metrics.TotalBytesProcessed = totalBytesProcessed
	metrics.ErrorCount = errorCount

	if totalOps > 0 {
		metrics.CacheHitRate = float64(totalCacheHits) / float64(totalOps) * 100.0
	}

	// Copy operation metrics
	for _, op := range m.operations {
		opCopy := *op
		metrics.OperationMetrics = append(metrics.OperationMetrics, &opCopy)
	}

	// Copy resource samples
	copy(metrics.ResourceSamples, m.resourceSamples)

	// Calculate resource statistics
	if len(m.resourceSamples) > 0 {
		totalMemory := int64(0)
		totalCPU := 0.0
		peakMemory := int64(0)
		peakCPU := 0.0

		for _, sample := range m.resourceSamples {
			totalMemory += sample.MemoryMB
			totalCPU += sample.CPUPercent
			
			if sample.MemoryMB > peakMemory {
				peakMemory = sample.MemoryMB
			}
			if sample.CPUPercent > peakCPU {
				peakCPU = sample.CPUPercent
			}
		}

		sampleCount := int64(len(m.resourceSamples))
		metrics.AvgMemoryMB = totalMemory / sampleCount
		metrics.AvgCPUPercent = totalCPU / float64(sampleCount)
		metrics.PeakMemoryMB = peakMemory
		metrics.PeakCPUPercent = peakCPU
	}

	return metrics
}

// Finish completes metrics collection
func (m *MetricsCollector) Finish(success bool) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	endTime := time.Now()
	m.endTime = &endTime

	// Stop resource sampling
	m.stopResourceSampling()
}

// startResourceSampling starts periodic resource usage sampling
func (m *MetricsCollector) startResourceSampling() {
	m.samplingTicker = time.NewTicker(5 * time.Second) // Sample every 5 seconds
	
	go func() {
		for {
			select {
			case <-m.samplingTicker.C:
				m.sampleResources()
			case <-m.stopSampling:
				return
			}
		}
	}()
}

// stopResourceSampling stops resource usage sampling
func (m *MetricsCollector) stopResourceSampling() {
	if m.samplingTicker != nil {
		m.samplingTicker.Stop()
	}
	
	select {
	case m.stopSampling <- true:
	default:
	}
}

// sampleResources takes a resource usage sample
func (m *MetricsCollector) sampleResources() {
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	sample := ResourceSample{
		Timestamp:      time.Now(),
		MemoryMB:       int64(memStats.Alloc / 1024 / 1024),
		GoroutineCount: runtime.NumGoroutine(),
		GCPauseMS:      float64(memStats.PauseNs[(memStats.NumGC+255)%256]) / 1000000,
	}

	// Note: CPU and disk/network I/O would require platform-specific code
	// For now, we'll set them to 0 and they can be implemented later
	sample.CPUPercent = 0.0
	sample.DiskUsageMB = 0
	sample.NetworkRxMB = 0
	sample.NetworkTxMB = 0

	m.mutex.Lock()
	m.resourceSamples = append(m.resourceSamples, sample)
	
	// Keep only the last 1000 samples to prevent memory growth
	if len(m.resourceSamples) > 1000 {
		m.resourceSamples = m.resourceSamples[len(m.resourceSamples)-1000:]
	}
	m.mutex.Unlock()
}

// getStageKey generates a unique key for a stage
func (m *MetricsCollector) getStageKey(stageName string, platform string) string {
	if platform != "" {
		return fmt.Sprintf("%s@%s", stageName, platform)
	}
	return stageName
}