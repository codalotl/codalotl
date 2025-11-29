package gograph

// StronglyConnectedComponents computes and returns the strongly connected components of the graph.
//
// A strongly connected component of a directed graph is a maximal subgraph where for any two vertices u and v in the subgraph, there is a path from u to v and a path from v to u. In
// simpler terms, it's a set of nodes where every node is reachable from every other node by following connections forwards.
//
// The return value is a slice of connected components, where each component is a map of identifiers. A given identifier will only appear in one component.
func (g *Graph) StronglyConnectedComponents() []map[string]struct{} {
	nodes := g.identifiers

	if len(nodes) == 0 {
		return nil
	}

	scc := &sccState{
		graph:   g.intraUses,
		nodes:   nodes,
		disc:    make(map[string]int),
		low:     make(map[string]int),
		onStack: make(map[string]bool),
	}

	for node := range nodes {
		if _, visited := scc.disc[node]; !visited {
			scc.find(node)
		}
	}

	return scc.sccs
}

type sccState struct {
	graph map[string]map[string]struct{}
	nodes map[string]struct{}

	sccs    []map[string]struct{}
	stack   []string
	onStack map[string]bool
	disc    map[string]int
	low     map[string]int
	index   int
}

func (s *sccState) find(u string) {
	s.disc[u] = s.index
	s.low[u] = s.index
	s.index++
	s.stack = append(s.stack, u)
	s.onStack[u] = true

	for v := range s.graph[u] {
		if _, visited := s.disc[v]; !visited {
			s.find(v)
			if s.low[v] < s.low[u] {
				s.low[u] = s.low[v]
			}
		} else if s.onStack[v] {
			if s.disc[v] < s.low[u] {
				s.low[u] = s.disc[v]
			}
		}
	}

	if s.low[u] == s.disc[u] {
		component := make(map[string]struct{})
		for {
			w := s.stack[len(s.stack)-1]
			s.stack = s.stack[:len(s.stack)-1]
			s.onStack[w] = false
			component[w] = struct{}{}
			if u == w {
				break
			}
		}
		s.sccs = append(s.sccs, component)
	}
}

// WeaklyConnectedComponents returns all weakly connected components of g.
//
// A weakly connected component is a maximal set of vertices that remain mutually reachable when edge direction is ignored-that is, in the underlying undirected graph every vertex can
// reach every other.
//
// When analysing source code, this reveals clusters of declarations (functions, types, variables) that are logically tied together and should usually be refactored or moved as a unit.
//
// The result is a slice in which each element is a component represented as a set of vertex identifiers (map[string]struct{}). An identifier appears in exactly one component.
func (g *Graph) WeaklyConnectedComponents() []map[string]struct{} {
	nodes := g.identifiers

	if len(nodes) == 0 {
		return nil
	}

	// Build an undirected graph representation from intraUses.
	adj := make(map[string]map[string]struct{})
	for u, vs := range g.intraUses {
		if _, ok := adj[u]; !ok {
			adj[u] = make(map[string]struct{})
		}
		for v := range vs {
			if _, ok := adj[v]; !ok {
				adj[v] = make(map[string]struct{})
			}
			adj[u][v] = struct{}{}
			adj[v][u] = struct{}{}
		}
	}

	var components []map[string]struct{}
	visited := make(map[string]struct{})

	for node := range nodes {
		if _, ok := visited[node]; ok {
			continue
		}

		component := make(map[string]struct{})
		stack := []string{node}
		visited[node] = struct{}{}

		for len(stack) > 0 {
			curr := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			component[curr] = struct{}{}

			for neighbor := range adj[curr] {
				if _, ok := visited[neighbor]; !ok {
					visited[neighbor] = struct{}{}
					stack = append(stack, neighbor)
				}
			}
		}
		components = append(components, component)
	}

	return components
}
