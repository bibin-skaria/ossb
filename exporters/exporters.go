package exporters

import (
	"fmt"
	"github.com/bibin-skaria/ossb/internal/types"
)

type Exporter interface {
	Export(result *types.BuildResult, config *types.BuildConfig, workDir string) error
}

var exporters = make(map[string]Exporter)

func RegisterExporter(name string, exporter Exporter) {
	exporters[name] = exporter
}

func GetExporter(name string) (Exporter, error) {
	exporter, exists := exporters[name]
	if !exists {
		return nil, fmt.Errorf("exporter %s not found", name)
	}
	return exporter, nil
}

func ListExporters() []string {
	names := make([]string, 0, len(exporters))
	for name := range exporters {
		names = append(names, name)
	}
	return names
}