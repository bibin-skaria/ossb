package engine

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"time"

	"github.com/bibin-skaria/ossb/internal/types"
)

// ResourceManager manages system resources and enforces limits during builds
type ResourceManager struct {
	mutex           sync.RWMutex
	limits          *types.ResourceLimits
	currentUsage    *ResourceUsage
	monitors        map[string]*ResourceMonitor
	semaphores      map[string]*Semaphore
	config          *ResourceConfig
	profiler        *ResourceProfiler
	optimizer       *ResourceOptimizer
}

// ResourceUsage tracks current resource consumption
type ResourceUsage struct {
	MemoryMB        int64     `json:"memory_mb"`
	CPUPercent      float64   `json:"cpu_percent"`
	DiskUsageMB     int64     `json:"disk_usage_mb"`
	NetworkRxMB     int64     `json:"network_rx_mb"`
	NetworkTxMB     int64     `json:"network_tx_mb"`
	GoroutineCount  int       `json:"goroutine_count"`
	OpenFiles       int       `json:"open_files"`
	LastUpdated     time.Time `json:"last_updated"`
}

// ResourceMonitor monitors resource usage for a specific build or operation
type ResourceMonitor struct {
	ID              string
	StartTime       time.Time
	PeakMemoryMB    int64
	PeakCPUPercent  float64
	TotalDiskIO     int64
	TotalNetworkIO  int64
	Samples         []ResourceSampleRM
	ticker          *time.Ticker
	stopChan        chan bool
	mutex           sync.RWMutex
}

// ResourceSampleRM represents a point-in-time resource measurement for resource manager
type ResourceSampleRM struct {
	Timestamp      time.Time `json:"timestamp"`
	MemoryMB       int64     `json:"memory_mb"`
	CPUPercent     float64   `json:"cpu_percent"`
	DiskUsageMB    int64     `json:"disk_usage_mb"`
	NetworkRxMB    int64     `json:"network_rx_mb"`
	NetworkTxMB    int64     `json:"network_tx_mb"`
	GoroutineCount int       `json:"goroutine_count"`
	GCPauseMS      float64   `json:"gc_pause_ms"`
}

// Semaphore controls concurrent access to resources
type Semaphore struct {
	permits chan struct{}
	name    string
}

// ResourceConfig defines resource management configuration
type ResourceConfig struct {
	MaxConcurrentBuilds    int           `json:"max_concurrent_builds"`
	MaxConcurrentOps       int           `json:"max_concurrent_ops"`
	MemoryLimitMB          int64         `json:"memory_limit_mb"`
	CPULimitPercent        float64       `json:"cpu_limit_percent"`
	DiskLimitMB            int64         `json:"disk_limit_mb"`
	SamplingInterval       time.Duration `json:"sampling_interval"`
	ResourceCheckInterval  time.Duration `json:"resource_check_interval"`
	GCThresholdMB          int64         `json:"gc_threshold_mb"`
	EnableOptimizations    bool          `json:"enable_optimizations"`
	EnableProfiling        bool          `json:"enable_profiling"`
}

// ResourceProfiler profiles resource usage patterns
type ResourceProfiler struct {
	profiles map[string]*ResourceProfile
	mutex    sync.RWMutex
}

// ResourceProfile contains profiling data for a specific operation type
type ResourceProfile struct {
	OperationType   types.OperationType `json:"operation_type"`
	SampleCount     int                 `json:"sample_count"`
	AvgMemoryMB     int64               `json:"avg_memory_mb"`
	AvgDurationMS   int64               `json:"avg_duration_ms"`
	AvgCPUPercent   float64             `json:"avg_cpu_percent"`
	PeakMemoryMB    int64               `json:"peak_memory_mb"`
	PeakCPUPercent  float64             `json:"peak_cpu_percent"`
	LastUpdated     time.Time           `json:"last_updated"`
}

// ResourceOptimizer optimizes resource usage based on profiling data
type ResourceOptimizer struct {
	profiles        map[string]*ResourceProfile
	optimizations   []OptimizationRule
	mutex           sync.RWMutex
}

// OptimizationRule defines a resource optimization rule
type OptimizationRule struct {
	Name        string                                    `json:"name"`
	Condition   func(*ResourceUsage, *ResourceProfile) bool `json:"-"`
	Action      func(*ResourceManager) error              `json:"-"`
	Description string                                    `json:"description"`
	Enabled     bool                                      `json:"enabled"`
}

// NewResourceManager creates a new resource manager
func NewResourceManager(limits *types.ResourceLimits) *ResourceManager {
	config := &ResourceConfig{
		MaxConcurrentBuilds:   runtime.NumCPU(),
		MaxConcurrentOps:      runtime.NumCPU() * 2,
		MemoryLimitMB:         4096, // 4GB default
		CPULimitPercent:       80.0,
		DiskLimitMB:           10240, // 10GB default
		SamplingInterval:      5 * time.Second,
		ResourceCheckInterval: 30 * time.Second,
		GCThresholdMB:         1024, // 1GB
		EnableOptimizations:   true,
		EnableProfiling:       true,
	}

	// Override defaults with provided limits
	if limits != nil {
		if limits.Memory != "" {
			if memMB, err := parseMemoryString(limits.Memory); err == nil {
				config.MemoryLimitMB = memMB
			}
		}
		if limits.CPU != "" {
			if cpuPercent, err := parseCPUString(limits.CPU); err == nil {
				config.CPULimitPercent = cpuPercent
			}
		}
		if limits.Disk != "" {
			if diskMB, err := parseMemoryString(limits.Disk); err == nil {
				config.DiskLimitMB = diskMB
			}
		}
	}

	rm := &ResourceManager{
		limits:       limits,
		currentUsage: &ResourceUsage{},
		monitors:     make(map[string]*ResourceMonitor),
		semaphores:   make(map[string]*Semaphore),
		config:       config,
		profiler:     NewResourceProfiler(),
		optimizer:    NewResourceOptimizer(),
	}

	// Create semaphores
	rm.semaphores["builds"] = NewSemaphore("builds", config.MaxConcurrentBuilds)
	rm.semaphores["operations"] = NewSemaphore("operations", config.MaxConcurrentOps)

	// Start resource monitoring
	go rm.startResourceMonitoring()

	return rm
}

// NewSemaphore creates a new semaphore with the specified capacity
func NewSemaphore(name string, capacity int) *Semaphore {
	return &Semaphore{
		permits: make(chan struct{}, capacity),
		name:    name,
	}
}

// Acquire acquires a permit from the semaphore
func (s *Semaphore) Acquire(ctx context.Context) error {
	select {
	case s.permits <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Release releases a permit back to the semaphore
func (s *Semaphore) Release() {
	select {
	case <-s.permits:
	default:
		// Should not happen in normal operation
	}
}

// AcquireBuildPermit acquires a permit for a build operation
func (rm *ResourceManager) AcquireBuildPermit(ctx context.Context) error {
	return rm.semaphores["builds"].Acquire(ctx)
}

// ReleaseBuildPermit releases a build permit
func (rm *ResourceManager) ReleaseBuildPermit() {
	rm.semaphores["builds"].Release()
}

// AcquireOperationPermit acquires a permit for an operation
func (rm *ResourceManager) AcquireOperationPermit(ctx context.Context) error {
	return rm.semaphores["operations"].Acquire(ctx)
}

// ReleaseOperationPermit releases an operation permit
func (rm *ResourceManager) ReleaseOperationPermit() {
	rm.semaphores["operations"].Release()
}

// StartMonitoring starts monitoring resources for a specific build or operation
func (rm *ResourceManager) StartMonitoring(id string) *ResourceMonitor {
	rm.mutex.Lock()
	defer rm.mutex.Unlock()

	monitor := &ResourceMonitor{
		ID:        id,
		StartTime: time.Now(),
		Samples:   make([]ResourceSampleRM, 0),
		ticker:    time.NewTicker(rm.config.SamplingInterval),
		stopChan:  make(chan bool, 1),
	}

	rm.monitors[id] = monitor

	// Start sampling
	go monitor.startSampling()

	return monitor
}

// StopMonitoring stops monitoring for a specific ID
func (rm *ResourceManager) StopMonitoring(id string) *ResourceMonitor {
	rm.mutex.Lock()
	defer rm.mutex.Unlock()

	monitor, exists := rm.monitors[id]
	if !exists {
		return nil
	}

	monitor.stop()
	delete(rm.monitors, id)

	return monitor
}

// CheckResourceLimits checks if current resource usage exceeds limits
func (rm *ResourceManager) CheckResourceLimits() error {
	rm.mutex.RLock()
	defer rm.mutex.RUnlock()

	usage := rm.getCurrentUsage()

	if rm.config.MemoryLimitMB > 0 && usage.MemoryMB > rm.config.MemoryLimitMB {
		return fmt.Errorf("memory usage (%d MB) exceeds limit (%d MB)", usage.MemoryMB, rm.config.MemoryLimitMB)
	}

	if rm.config.CPULimitPercent > 0 && usage.CPUPercent > rm.config.CPULimitPercent {
		return fmt.Errorf("CPU usage (%.1f%%) exceeds limit (%.1f%%)", usage.CPUPercent, rm.config.CPULimitPercent)
	}

	if rm.config.DiskLimitMB > 0 && usage.DiskUsageMB > rm.config.DiskLimitMB {
		return fmt.Errorf("disk usage (%d MB) exceeds limit (%d MB)", usage.DiskUsageMB, rm.config.DiskLimitMB)
	}

	return nil
}

// OptimizeResources applies resource optimizations
func (rm *ResourceManager) OptimizeResources() error {
	if !rm.config.EnableOptimizations {
		return nil
	}

	rm.mutex.Lock()
	defer rm.mutex.Unlock()

	usage := rm.getCurrentUsage()

	// Trigger garbage collection if memory usage is high
	if usage.MemoryMB > rm.config.GCThresholdMB {
		runtime.GC()
		runtime.GC() // Double GC for better cleanup
	}

	// Apply optimizer rules
	return rm.optimizer.ApplyOptimizations(rm, usage)
}

// GetResourceUsage returns current resource usage
func (rm *ResourceManager) GetResourceUsage() *ResourceUsage {
	rm.mutex.RLock()
	defer rm.mutex.RUnlock()

	return rm.getCurrentUsage()
}

// GetMonitoringData returns monitoring data for all active monitors
func (rm *ResourceManager) GetMonitoringData() map[string]*ResourceMonitor {
	rm.mutex.RLock()
	defer rm.mutex.RUnlock()

	result := make(map[string]*ResourceMonitor)
	for id, monitor := range rm.monitors {
		// Create a copy to avoid race conditions
		monitorCopy := *monitor
		monitorCopy.Samples = make([]ResourceSampleRM, len(monitor.Samples))
		copy(monitorCopy.Samples, monitor.Samples)
		result[id] = &monitorCopy
	}

	return result
}

// ProfileOperation profiles resource usage for an operation
func (rm *ResourceManager) ProfileOperation(opType types.OperationType, duration time.Duration, peakMemory int64, peakCPU float64) {
	if !rm.config.EnableProfiling {
		return
	}

	rm.profiler.RecordOperation(opType, duration, peakMemory, peakCPU)
}

// GetResourceProfile returns resource profile for an operation type
func (rm *ResourceManager) GetResourceProfile(opType types.OperationType) *ResourceProfile {
	return rm.profiler.GetProfile(opType)
}

// startResourceMonitoring starts the main resource monitoring loop
func (rm *ResourceManager) startResourceMonitoring() {
	ticker := time.NewTicker(rm.config.ResourceCheckInterval)
	defer ticker.Stop()

	for range ticker.C {
		// Update current usage
		rm.mutex.Lock()
		rm.currentUsage = rm.getCurrentUsage()
		rm.mutex.Unlock()

		// Check limits and optimize if needed
		if err := rm.CheckResourceLimits(); err != nil {
			// Log warning but don't fail the build
			fmt.Printf("Warning: %v\n", err)
		}

		// Apply optimizations
		if err := rm.OptimizeResources(); err != nil {
			fmt.Printf("Warning: optimization failed: %v\n", err)
		}
	}
}

// getCurrentUsage gets current system resource usage
func (rm *ResourceManager) getCurrentUsage() *ResourceUsage {
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	usage := &ResourceUsage{
		MemoryMB:       int64(memStats.Alloc / 1024 / 1024),
		GoroutineCount: runtime.NumGoroutine(),
		LastUpdated:    time.Now(),
	}

	// Note: CPU, disk, and network monitoring would require platform-specific code
	// For now, we'll set them to 0 and they can be implemented later with proper
	// system monitoring libraries like gopsutil
	usage.CPUPercent = 0.0
	usage.DiskUsageMB = 0
	usage.NetworkRxMB = 0
	usage.NetworkTxMB = 0
	usage.OpenFiles = 0

	return usage
}

// startSampling starts resource sampling for a monitor
func (m *ResourceMonitor) startSampling() {
	for {
		select {
		case <-m.ticker.C:
			m.takeSample()
		case <-m.stopChan:
			return
		}
	}
}

// takeSample takes a resource usage sample
func (m *ResourceMonitor) takeSample() {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	sample := ResourceSampleRM{
		Timestamp:      time.Now(),
		MemoryMB:       int64(memStats.Alloc / 1024 / 1024),
		GoroutineCount: runtime.NumGoroutine(),
		GCPauseMS:      float64(memStats.PauseNs[(memStats.NumGC+255)%256]) / 1000000,
	}

	// Update peak values
	if sample.MemoryMB > m.PeakMemoryMB {
		m.PeakMemoryMB = sample.MemoryMB
	}
	if sample.CPUPercent > m.PeakCPUPercent {
		m.PeakCPUPercent = sample.CPUPercent
	}

	m.Samples = append(m.Samples, sample)

	// Keep only the last 1000 samples to prevent memory growth
	if len(m.Samples) > 1000 {
		m.Samples = m.Samples[len(m.Samples)-1000:]
	}
}

// stop stops the resource monitor
func (m *ResourceMonitor) stop() {
	if m.ticker != nil {
		m.ticker.Stop()
	}

	select {
	case m.stopChan <- true:
	default:
	}
}

// GetSummary returns a summary of the monitoring data
func (m *ResourceMonitor) GetSummary() *ResourceMonitorSummary {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	if len(m.Samples) == 0 {
		return &ResourceMonitorSummary{
			ID:        m.ID,
			StartTime: m.StartTime,
			Duration:  time.Since(m.StartTime),
		}
	}

	var totalMemory int64
	var totalCPU float64
	var totalGC float64

	for _, sample := range m.Samples {
		totalMemory += sample.MemoryMB
		totalCPU += sample.CPUPercent
		totalGC += sample.GCPauseMS
	}

	sampleCount := int64(len(m.Samples))

	return &ResourceMonitorSummary{
		ID:             m.ID,
		StartTime:      m.StartTime,
		Duration:       time.Since(m.StartTime),
		SampleCount:    int(sampleCount),
		AvgMemoryMB:    totalMemory / sampleCount,
		PeakMemoryMB:   m.PeakMemoryMB,
		AvgCPUPercent:  totalCPU / float64(sampleCount),
		PeakCPUPercent: m.PeakCPUPercent,
		AvgGCPauseMS:   totalGC / float64(sampleCount),
		TotalDiskIO:    m.TotalDiskIO,
		TotalNetworkIO: m.TotalNetworkIO,
	}
}

// ResourceMonitorSummary provides a summary of resource monitoring data
type ResourceMonitorSummary struct {
	ID             string        `json:"id"`
	StartTime      time.Time     `json:"start_time"`
	Duration       time.Duration `json:"duration"`
	SampleCount    int           `json:"sample_count"`
	AvgMemoryMB    int64         `json:"avg_memory_mb"`
	PeakMemoryMB   int64         `json:"peak_memory_mb"`
	AvgCPUPercent  float64       `json:"avg_cpu_percent"`
	PeakCPUPercent float64       `json:"peak_cpu_percent"`
	AvgGCPauseMS   float64       `json:"avg_gc_pause_ms"`
	TotalDiskIO    int64         `json:"total_disk_io"`
	TotalNetworkIO int64         `json:"total_network_io"`
}

// NewResourceProfiler creates a new resource profiler
func NewResourceProfiler() *ResourceProfiler {
	return &ResourceProfiler{
		profiles: make(map[string]*ResourceProfile),
	}
}

// RecordOperation records profiling data for an operation
func (rp *ResourceProfiler) RecordOperation(opType types.OperationType, duration time.Duration, peakMemory int64, peakCPU float64) {
	rp.mutex.Lock()
	defer rp.mutex.Unlock()

	key := string(opType)
	profile, exists := rp.profiles[key]
	if !exists {
		profile = &ResourceProfile{
			OperationType: opType,
			LastUpdated:   time.Now(),
		}
		rp.profiles[key] = profile
	}

	// Update running averages
	profile.SampleCount++
	durationMS := duration.Milliseconds()

	if profile.SampleCount == 1 {
		profile.AvgMemoryMB = peakMemory
		profile.AvgDurationMS = durationMS
		profile.AvgCPUPercent = peakCPU
		profile.PeakMemoryMB = peakMemory
		profile.PeakCPUPercent = peakCPU
	} else {
		// Running average calculation
		profile.AvgMemoryMB = (profile.AvgMemoryMB*(int64(profile.SampleCount-1)) + peakMemory) / int64(profile.SampleCount)
		profile.AvgDurationMS = (profile.AvgDurationMS*(int64(profile.SampleCount-1)) + durationMS) / int64(profile.SampleCount)
		profile.AvgCPUPercent = (profile.AvgCPUPercent*float64(profile.SampleCount-1) + peakCPU) / float64(profile.SampleCount)

		// Update peaks
		if peakMemory > profile.PeakMemoryMB {
			profile.PeakMemoryMB = peakMemory
		}
		if peakCPU > profile.PeakCPUPercent {
			profile.PeakCPUPercent = peakCPU
		}
	}

	profile.LastUpdated = time.Now()
}

// GetProfile returns the resource profile for an operation type
func (rp *ResourceProfiler) GetProfile(opType types.OperationType) *ResourceProfile {
	rp.mutex.RLock()
	defer rp.mutex.RUnlock()

	if profile, exists := rp.profiles[string(opType)]; exists {
		// Return a copy to avoid race conditions
		profileCopy := *profile
		return &profileCopy
	}

	return nil
}

// GetAllProfiles returns all resource profiles
func (rp *ResourceProfiler) GetAllProfiles() map[string]*ResourceProfile {
	rp.mutex.RLock()
	defer rp.mutex.RUnlock()

	result := make(map[string]*ResourceProfile)
	for key, profile := range rp.profiles {
		profileCopy := *profile
		result[key] = &profileCopy
	}

	return result
}

// NewResourceOptimizer creates a new resource optimizer
func NewResourceOptimizer() *ResourceOptimizer {
	optimizer := &ResourceOptimizer{
		profiles: make(map[string]*ResourceProfile),
	}

	// Add default optimization rules
	optimizer.optimizations = []OptimizationRule{
		{
			Name:        "memory_gc_trigger",
			Description: "Trigger garbage collection when memory usage is high",
			Enabled:     true,
			Condition: func(usage *ResourceUsage, profile *ResourceProfile) bool {
				return usage.MemoryMB > 1024 // 1GB threshold
			},
			Action: func(rm *ResourceManager) error {
				runtime.GC()
				return nil
			},
		},
		{
			Name:        "reduce_concurrency",
			Description: "Reduce concurrent operations when resources are constrained",
			Enabled:     true,
			Condition: func(usage *ResourceUsage, profile *ResourceProfile) bool {
				return usage.MemoryMB > 2048 || usage.CPUPercent > 90.0
			},
			Action: func(rm *ResourceManager) error {
				// Reduce concurrent operations by half
				currentCap := cap(rm.semaphores["operations"].permits)
				if currentCap > 1 {
					newCap := currentCap / 2
					rm.semaphores["operations"] = NewSemaphore("operations", newCap)
				}
				return nil
			},
		},
	}

	return optimizer
}

// ApplyOptimizations applies optimization rules based on current resource usage
func (ro *ResourceOptimizer) ApplyOptimizations(rm *ResourceManager, usage *ResourceUsage) error {
	ro.mutex.RLock()
	defer ro.mutex.RUnlock()

	for _, rule := range ro.optimizations {
		if !rule.Enabled {
			continue
		}

		if rule.Condition(usage, nil) {
			if err := rule.Action(rm); err != nil {
				return fmt.Errorf("optimization rule '%s' failed: %v", rule.Name, err)
			}
		}
	}

	return nil
}

// Shutdown gracefully shuts down the resource manager
func (rm *ResourceManager) Shutdown(ctx context.Context) error {
	rm.mutex.Lock()
	defer rm.mutex.Unlock()

	// Stop all active monitors
	for id, monitor := range rm.monitors {
		monitor.stop()
		delete(rm.monitors, id)
	}

	return nil
}

// Helper functions for parsing resource strings
func parseMemoryString(memStr string) (int64, error) {
	// Simple implementation - in production, use a proper parser
	// that handles units like "1Gi", "512Mi", etc.
	if memStr == "" {
		return 0, fmt.Errorf("empty memory string")
	}

	// For now, assume it's in MB
	return 1024, nil // Default 1GB
}

func parseCPUString(cpuStr string) (float64, error) {
	// Simple implementation - in production, parse "1000m", "1.5", etc.
	if cpuStr == "" {
		return 0, fmt.Errorf("empty CPU string")
	}

	// For now, assume it's a percentage
	return 80.0, nil // Default 80%
}