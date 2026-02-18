package gograph

import "sort"

// LeavesOf returns the set of leaves of the graph, starting from each of idents. Any identifier in disconnectedIdents is considered not in the graph, turning a
// node depending on only disconnected identifiers into a leaf. True leaves are also considered leaves. If a node is returned because it only depends on disconnectedIdents,
// this function does not traverse into the disconnectedIdents. No disconnectedIdents can be returned by this function. If there are no leaves of an ident due to
// traversing into an SCC, each identifier in the SCC is returned.
func (g *Graph) LeavesOf(idents, disconnectedIdents []string) []string {
	disc := make(map[string]struct{}, len(disconnectedIdents))
	for _, id := range disconnectedIdents {
		disc[id] = struct{}{}
	}

	// -------- 1.  Build / complete the SCC list --------
	sccs := g.StronglyConnectedComponents()
	sccIdx := make(map[string]int, len(sccs))
	for i, c := range sccs {
		for v := range c {
			sccIdx[v] = i
		}
	}
	ensureSCC := func(v string) int {
		if i, ok := sccIdx[v]; ok {
			return i
		}
		i := len(sccs)
		sccs = append(sccs, map[string]struct{}{v: {}})
		sccIdx[v] = i
		return i
	}

	// -------- 2.  Build condensation DAG (edges between SCCs) --------
	out := make([]map[int]struct{}, len(sccs))
	for i := range out {
		out[i] = map[int]struct{}{}
	}
	for v, uses := range g.intraUses {
		if _, gone := disc[v]; gone {
			continue
		}
		from := ensureSCC(v)
		for dep := range uses {
			if _, gone := disc[dep]; gone {
				continue
			}
			to := ensureSCC(dep)
			if from != to {
				out[from][to] = struct{}{}
			}
		}
	}

	// -------- 3.  DFS with memoisation on the DAG --------
	memo := make([][]string, len(sccs))
	visited := make([]bool, len(sccs))
	var dfs func(int) []string

	dfs = func(i int) []string {
		if memo[i] != nil || visited[i] {
			return memo[i] // may be nil => “no leaves”
		}
		visited[i] = true

		if len(out[i]) == 0 { // terminal SCC in the DAG
			leafSet := make([]string, 0, len(sccs[i]))
			for v := range sccs[i] {
				if _, gone := disc[v]; !gone {
					leafSet = append(leafSet, v)
				}
			}
			sort.Strings(leafSet)
			memo[i] = leafSet
			return leafSet
		}

		uni := make(map[string]struct{})
		for j := range out[i] {
			for _, l := range dfs(j) {
				uni[l] = struct{}{}
			}
		}
		if len(uni) == 0 { // cycle(s) with no exit to real leaves
			return nil
		}
		leafs := make([]string, 0, len(uni))
		for l := range uni {
			leafs = append(leafs, l)
		}
		sort.Strings(leafs)
		memo[i] = leafs
		return leafs
	}

	// -------- 4.  Collect results for the requested idents --------
	res := make(map[string]struct{})
	for _, id := range idents {
		if _, gone := disc[id]; gone {
			continue
		}
		i := ensureSCC(id)
		if leaves := dfs(i); len(leaves) != 0 {
			for _, l := range leaves {
				res[l] = struct{}{}
			}
		} else { // no leaves reachable – return the whole SCC
			for v := range sccs[i] {
				if _, gone := disc[v]; !gone {
					res[v] = struct{}{}
				}
			}
		}
	}

	outSlice := make([]string, 0, len(res))
	for v := range res {
		outSlice = append(outSlice, v)
	}
	sort.Strings(outSlice)
	return outSlice
}
