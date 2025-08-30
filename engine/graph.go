package engine

import (
	"fmt"
	"github.com/bibin-skaria/ossb/internal/types"
)

type GraphSolver struct {
	graph *types.Graph
}

func NewGraphSolver() *GraphSolver {
	return &GraphSolver{
		graph: types.NewGraph(),
	}
}

func (gs *GraphSolver) BuildGraph(operations []*types.Operation) error {
	gs.graph = types.NewGraph()
	
	outputToNode := make(map[string]string)
	
	for i, op := range operations {
		nodeID := fmt.Sprintf("op-%d", i)
		gs.graph.AddNode(nodeID, op)
		
		for _, output := range op.Outputs {
			outputToNode[output] = nodeID
		}
	}

	for i, op := range operations {
		nodeID := fmt.Sprintf("op-%d", i)
		
		for _, input := range op.Inputs {
			if depNodeID, exists := outputToNode[input]; exists {
				if err := gs.graph.AddDependency(nodeID, depNodeID); err != nil {
					return fmt.Errorf("failed to add dependency: %v", err)
				}
			}
		}
	}

	if gs.graph.HasCycles() {
		return fmt.Errorf("dependency graph contains cycles")
	}

	gs.graph.Optimize()
	
	return nil
}

func (gs *GraphSolver) GetExecutionOrder() ([]string, error) {
	return gs.graph.TopologicalSort()
}

func (gs *GraphSolver) GetOperation(nodeID string) *types.Operation {
	if node, exists := gs.graph.Nodes[nodeID]; exists {
		return node.Operation
	}
	return nil
}

func (gs *GraphSolver) GetDependencies(nodeID string) []string {
	if node, exists := gs.graph.Nodes[nodeID]; exists {
		return node.Dependencies
	}
	return []string{}
}

func (gs *GraphSolver) GetDependents(nodeID string) []string {
	if node, exists := gs.graph.Nodes[nodeID]; exists {
		return node.Dependents
	}
	return []string{}
}

func (gs *GraphSolver) ValidateGraph() error {
	if gs.graph == nil {
		return fmt.Errorf("graph not initialized")
	}

	if len(gs.graph.Nodes) == 0 {
		return fmt.Errorf("graph is empty")
	}

	for nodeID, node := range gs.graph.Nodes {
		if node.Operation == nil {
			return fmt.Errorf("node %s has nil operation", nodeID)
		}

		for _, depID := range node.Dependencies {
			if _, exists := gs.graph.Nodes[depID]; !exists {
				return fmt.Errorf("node %s depends on non-existent node %s", nodeID, depID)
			}
		}

		for _, depID := range node.Dependents {
			if _, exists := gs.graph.Nodes[depID]; !exists {
				return fmt.Errorf("node %s has non-existent dependent %s", nodeID, depID)
			}
		}
	}

	if gs.graph.HasCycles() {
		return fmt.Errorf("graph contains cycles")
	}

	return nil
}

func (gs *GraphSolver) OptimizeGraph() {
	if gs.graph != nil {
		gs.graph.Optimize()
	}
}

func (gs *GraphSolver) GetNodeCount() int {
	if gs.graph == nil {
		return 0
	}
	return len(gs.graph.Nodes)
}

func (gs *GraphSolver) GetGraph() *types.Graph {
	return gs.graph
}