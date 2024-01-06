package expr

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"gonum.org/v1/gonum/graph/simple"
	"gonum.org/v1/gonum/graph/topo"

	"github.com/xquare-dashboard/pkg/expr/mathexp"
)

// NodeType is the type of a DPNode. Currently either a expression command or datasources query.
type NodeType int

const (
	// TypeCMDNode is a NodeType for expression commands.
	TypeCMDNode NodeType = iota
	// TypeDatasourceNode is a NodeType for datasources queries.
	TypeDatasourceNode
	// TypeMLNode is a NodeType for Machine Learning queries.
	TypeMLNode
)

func (nt NodeType) String() string {
	switch nt {
	case TypeCMDNode:
		return "Expression"
	case TypeDatasourceNode:
		return "Datasource"
	case TypeMLNode:
		return "Machine Learning"
	default:
		return "Unknown"
	}
}

// Node is a node in a Data Pipeline. Node is either a expression command or a datasources query.
type Node interface {
	ID() int64 // ID() allows the gonum graph node interface to be fulfilled
	NodeType() NodeType
	RefID() string
	String() string
	NeedsVars() []string
}

type ExecutableNode interface {
	Node
	Execute(ctx context.Context, now time.Time, vars mathexp.Vars, s *Service) (mathexp.Results, error)
}

// DataPipeline is an ordered set of nodes returned from DPGraph processing.
type DataPipeline []Node

// execute runs all the command/datasources requests in the pipeline return a
// map of the refId of the of each command
func (dp *DataPipeline) execute(c context.Context, now time.Time, s *Service) (mathexp.Vars, error) {
	vars := make(mathexp.Vars)
	for _, node := range *dp {
		execNode, ok := node.(ExecutableNode)
		if !ok {
			return vars, makeUnexpectedNodeTypeError(node.RefID(), node.NodeType().String())
		}

		res, err := execNode.Execute(c, now, vars, s)
		if err != nil {
			res.Error = err
		}

		vars[node.RefID()] = res
	}
	return vars, nil
}

// BuildPipeline builds a graph of the nodes, and returns the nodes in an
// executable order.
func (s *Service) buildPipeline(req *Request) (DataPipeline, error) {
	if req != nil && len(req.Headers) == 0 {
		req.Headers = map[string]string{}
	}

	graph, err := s.buildDependencyGraph(req)
	if err != nil {
		return nil, err
	}

	nodes, err := buildExecutionOrder(graph)
	if err != nil {
		return nil, err
	}

	return nodes, nil
}

// buildDependencyGraph returns a dependency graph for a set of queries.
func (s *Service) buildDependencyGraph(req *Request) (*simple.DirectedGraph, error) {
	graph, err := s.buildGraph(req)
	if err != nil {
		return nil, err
	}

	registry := buildNodeRegistry(graph)
	if err := buildGraphEdges(graph, registry); err != nil {
		return nil, err
	}

	return graph, nil
}

// buildExecutionOrder returns a sequence of nodes ordered by dependency.
// Note: During execution, Datasource query nodes for the same datasources will
// be grouped into one request and executed first as phase after this call.
func buildExecutionOrder(graph *simple.DirectedGraph) ([]Node, error) {
	sortedNodes, err := topo.SortStabilized(graph, nil)
	if err != nil {
		return nil, err
	}

	nodes := make([]Node, len(sortedNodes))
	for i, v := range sortedNodes {
		nodes[i] = v.(Node)
	}

	return nodes, nil
}

// buildNodeRegistry returns a lookup table for reference IDs to respective node.
func buildNodeRegistry(g *simple.DirectedGraph) map[string]Node {
	res := make(map[string]Node)

	nodeIt := g.Nodes()

	for nodeIt.Next() {
		if dpNode, ok := nodeIt.Node().(Node); ok {
			res[dpNode.RefID()] = dpNode
		}
	}

	return res
}

// buildGraph creates a new graph populated with nodes for every query.
func (s *Service) buildGraph(req *Request) (*simple.DirectedGraph, error) {
	dp := simple.NewDirectedGraph()

	for i, query := range req.Queries {
		if query.DataSource == nil {
			return nil, fmt.Errorf("missing datasources uid in query with refId %v", query.RefID)
		}

		rawQueryProp := make(map[string]any)
		queryBytes, err := query.JSON.MarshalJSON()

		if err != nil {
			return nil, err
		}

		err = json.Unmarshal(queryBytes, &rawQueryProp)
		if err != nil {
			return nil, err
		}

		rn := &rawNode{
			Query:      rawQueryProp,
			TimeRange:  query.TimeRange,
			DataSource: query.DataSource,
			idx:        int64(i),
		}

		var node Node
		node, err = s.buildDSNode(dp, rn, req)

		if err != nil {
			return nil, err
		}

		dp.AddNode(node)
	}
	return dp, nil
}

// buildGraphEdges generates graph edges based on each node's dependencies.
func buildGraphEdges(dp *simple.DirectedGraph, registry map[string]Node) error {
	nodeIt := dp.Nodes()

	for nodeIt.Next() {
		node := nodeIt.Node().(Node)

		if node.NodeType() != TypeCMDNode {
			// datasources node, nothing to do for now. Although if we want expression results to be
			// used as datasources query params some day this will need change
			continue
		}

		cmdNode := node.(*CMDNode)

		for _, neededVar := range cmdNode.Command.NeedsVars() {
			neededNode, ok := registry[neededVar]
			if !ok {
				return fmt.Errorf("unable to find dependent node '%v'", neededVar)
			}

			if neededNode.ID() == cmdNode.ID() {
				return fmt.Errorf("expression '%v' cannot reference itself. Must be query or another expression", neededVar)
			}

			if cmdNode.CMDType == TypeClassicConditions {
				if neededNode.NodeType() != TypeDatasourceNode {
					return fmt.Errorf("only data source queries may be inputs to a classic condition, %v is a %v", neededVar, neededNode.NodeType())
				}
			}

			if neededNode.NodeType() == TypeCMDNode {
				if neededNode.(*CMDNode).CMDType == TypeClassicConditions {
					return fmt.Errorf("classic conditions may not be the input for other expressions, but %v is the input for %v", neededVar, cmdNode.RefID())
				}
			}

			edge := dp.NewEdge(neededNode, cmdNode)

			dp.SetEdge(edge)
		}
	}
	return nil
}

// GetCommandsFromPipeline traverses the pipeline and extracts all CMDNode commands that match the type
func GetCommandsFromPipeline[T Command](pipeline DataPipeline) []T {
	var results []T
	for _, p := range pipeline {
		if p.NodeType() != TypeCMDNode {
			continue
		}
		switch cmd := p.(type) {
		case *CMDNode:
			switch r := cmd.Command.(type) {
			case T:
				results = append(results, r)
			}
		default:
			continue
		}
	}
	return results
}