package balancing

import "load_balancer/loadstats"

type nodePicker struct {
	stickyConnections bool
	nodes             []node
	loadStatistics    []loadstats.LoadStatistics
}

func (p nodePicker) Pick() node {
	//TODO implement
	return p.nodes[0]
}
