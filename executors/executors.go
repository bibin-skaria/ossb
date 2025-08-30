package executors

import (
	"fmt"
	"github.com/bibin-skaria/ossb/internal/types"
)

type Executor interface {
	Execute(operation *types.Operation, workDir string) (*types.OperationResult, error)
}

var executors = make(map[string]Executor)

func RegisterExecutor(name string, executor Executor) {
	executors[name] = executor
}

func GetExecutor(name string) (Executor, error) {
	executor, exists := executors[name]
	if !exists {
		return nil, fmt.Errorf("executor %s not found", name)
	}
	return executor, nil
}

func ListExecutors() []string {
	names := make([]string, 0, len(executors))
	for name := range executors {
		names = append(names, name)
	}
	return names
}