package balancing

import (
	"math"
	"sort"
	"time"
)

type nodePicker struct {
	stickyConnections bool
	loadStatistics    []LoadStatistics
}

//least connections out of all nodes order index * 0.15 (in other words, sort the amount of remaining connections per node
//in ascending order and take the node's index from that array)
//+
//percent of available connections * 0.15 (if max_conns == 30 and there are 15 active conns, this would be 50%)
//+
//(if config.stickyConnections is true) 0.15 if hash(client.address) matches current node else 0
//+
//lowest max response time order index * 0.08
//+
//lowest average response time order index * 0.13
//+
//lowest average deviation order index * 0.04
//+
//lowest CPU utilization order index * 0.15
//+
//highest free memory order index * 0.15

func (p *nodePicker) Pick() node {
	resultMap := make(map[uint]float32)
	//least connections
	sort.Slice(p.loadStatistics, func(i, j int) bool {
		return p.loadStatistics[i].connections > p.loadStatistics[j].connections
	})
	for i, stats := range p.loadStatistics {
		resultMap[stats.node.id] = float32(i + 1) * float32(0.15)
	}
	//percent available connections
	sort.Slice(p.loadStatistics, func(i, j int) bool {
		iMaxConns := p.loadStatistics[i].node.maxConnections
		jMaxConns := p.loadStatistics[j].node.maxConnections
		iPercentConns := float32(int(p.loadStatistics[i].connections) / iMaxConns) * float32(100)
		jPercentConns := float32(int(p.loadStatistics[j].connections) / jMaxConns) * float32(100)
		return iPercentConns > jPercentConns
	})
	for i, stats := range p.loadStatistics {
		resultMap[stats.node.id] += float32(i + 1) * float32(0.15)
	}
	//source IP hash
	if p.stickyConnections {
		for _, stats := range p.loadStatistics {
			if stats.matchesSourceHash {
				resultMap[stats.node.id] += float32(0.15)
			}
		}
	}
	//max response time
	sort.Slice(p.loadStatistics, func(i, j int) bool {
		iResponseTimes := p.loadStatistics[i].responseTimes
		jResponseTimes := p.loadStatistics[j].responseTimes
		iMaxTime := p.findMaxTime(iResponseTimes)
		jMaxTime := p.findMaxTime(jResponseTimes)
		return iMaxTime > jMaxTime
	})
	for i, stats := range p.loadStatistics {
		resultMap[stats.node.id] += float32(i + 1) * float32(0.08)
	}
	//avg response time
	sort.Slice(p.loadStatistics, func(i, j int) bool {
		iResponseTimes := p.loadStatistics[i].responseTimes
		jResponseTimes := p.loadStatistics[j].responseTimes
		iAvgTime := p.findAvgTime(iResponseTimes)
		jAvgTime := p.findAvgTime(jResponseTimes)
		return iAvgTime > jAvgTime
	})
	for i, stats := range p.loadStatistics {
		resultMap[stats.node.id] += float32(i + 1) * float32(0.13)
	}
	//std dev in response time
	sort.Slice(p.loadStatistics, func(i, j int) bool {
		iResponseTimes := p.loadStatistics[i].responseTimes
		jResponseTimes := p.loadStatistics[j].responseTimes
		iStdDev := p.findStdDev(iResponseTimes)
		jStdDev := p.findStdDev(jResponseTimes)
		return iStdDev > jStdDev
	})
	for i, stats := range p.loadStatistics {
		resultMap[stats.node.id] += float32(i + 1) * float32(0.04)
	}
	//cpu utilization
	sort.Slice(p.loadStatistics, func(i, j int) bool {
		return p.loadStatistics[i].usedResources.CpuUtilization > p.loadStatistics[j].usedResources.CpuUtilization
	})
	for i, stats := range p.loadStatistics {
		resultMap[stats.node.id] += float32(i + 1) * float32(0.15)
	}
	//free memory
	sort.Slice(p.loadStatistics, func(i, j int) bool {
		return p.loadStatistics[i].usedResources.FreeMemory < p.loadStatistics[j].usedResources.FreeMemory
	})
	for i, stats := range p.loadStatistics {
		resultMap[stats.node.id] += float32(i + 1) * float32(0.15)
	}

	keys := make([]uint, 0, len(resultMap))
	for k := range resultMap {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		return resultMap[keys[i]] > resultMap[keys[j]]
	})

	for _, stats := range p.loadStatistics {
		if stats.node.id == keys[0] {
			return stats.node
		}
	}
	return p.loadStatistics[0].node
}

func (p *nodePicker) findMaxTime(arr []time.Duration) time.Duration {
	var max time.Duration
	for _, el := range arr {
		if el > max {
			max = el
		}
	}
	return max
}

func (p *nodePicker) findAvgTime(arr []time.Duration) float64 {
	var sum time.Duration
	for _, el := range arr {
		sum += el
	}
	return float64(sum) / float64(len(arr))
}

func (p *nodePicker) findStdDev(arr []time.Duration) float64 {
	n := len(arr)
	mean := p.findAvgTime(arr)
	var sum float64
	for _, el := range arr {
		sum += math.Pow(float64(el) - mean, 2.0)
	}
	varience := sum / float64(n - 1)
	return math.Sqrt(varience)
}

func (p *nodePicker) index(arr []LoadStatistics, el node) int {
	for i, stats := range arr {
		if stats.node.id == el.id {
			return i
		}
	}
	return -1
}
