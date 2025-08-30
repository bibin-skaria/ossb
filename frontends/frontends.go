package frontends

import (
	"fmt"
	"github.com/bibin-skaria/ossb/internal/types"
)

type Frontend interface {
	Parse(dockerfile string, config *types.BuildConfig) ([]*types.Operation, error)
}

var frontends = make(map[string]Frontend)

func RegisterFrontend(name string, frontend Frontend) {
	frontends[name] = frontend
}

func GetFrontend(name string) (Frontend, error) {
	frontend, exists := frontends[name]
	if !exists {
		return nil, fmt.Errorf("frontend %s not found", name)
	}
	return frontend, nil
}

func ListFrontends() []string {
	names := make([]string, 0, len(frontends))
	for name := range frontends {
		names = append(names, name)
	}
	return names
}