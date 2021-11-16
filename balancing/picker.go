package balancing

import (
	"log"
	"math"
	"sort"
	"time"
)

type nodePicker struct {
	stickyConnections bool
	loadStatistics    []LoadStatistics
}

type formulaElement int

const (
	leastConnections formulaElement = iota
	percentAvailConnections
	sourceIpHashMatch
	lowestMaxResponseTime
	lowestAverageResponseTime
	lowestAverageDeviationInResponseTimes
	lowestCpuUtilization
	highestFreeMemory
)

var formulaCoefficients map[formulaElement]float32

func init() {
	formulaCoefficients = map[formulaElement]float32{
		leastConnections:                      float32(0.15),
		percentAvailConnections:               float32(0.15),
		sourceIpHashMatch:                     float32(0.15),
		lowestMaxResponseTime:                 float32(0.08),
		lowestAverageResponseTime:             float32(0.13),
		lowestAverageDeviationInResponseTimes: float32(0.04),
		lowestCpuUtilization:                  float32(0.15),
		highestFreeMemory:                     float32(0.15),
	}
}

//Pick select node with the highest value based on following formula:
// least connections out of all nodes order index * 0.15
// +
// percent of available connections * 0.15 (if max_conns == 30 and there are 15 active conns, this would be 50%)
// +
// (if config.stickyConnections is true) 0.15 if hash(client.address) matches current node else 0
// +
// lowest max response time order index * 0.08
// +
// lowest average response time order index * 0.13
// +
// lowest average deviation order index * 0.04
// +
// lowest CPU utilization order index * 0.15
// +
// highest free memory order index * 0.15
func (p *nodePicker) Pick() node {
	resultMap := make(map[uint]float32, len(p.loadStatistics))
	//least connections
	sort.SliceStable(p.loadStatistics, func(i, j int) bool {
		return p.loadStatistics[i].connections > p.loadStatistics[j].connections
	})
	for i, stats := range p.loadStatistics {
		resultMap[stats.node.id] = float32(i+1) * formulaCoefficients[leastConnections]
	}
	//percent available connections
	sort.SliceStable(p.loadStatistics, func(i, j int) bool {
		iMaxConns := p.loadStatistics[i].node.maxConnections
		jMaxConns := p.loadStatistics[j].node.maxConnections
		iPercentConns := float32(int(p.loadStatistics[i].connections)/iMaxConns) * float32(100)
		jPercentConns := float32(int(p.loadStatistics[j].connections)/jMaxConns) * float32(100)
		return iPercentConns > jPercentConns
	})
	for i, stats := range p.loadStatistics {
		resultMap[stats.node.id] += float32(i+1) * formulaCoefficients[percentAvailConnections]
	}
	//source IP hash
	if p.stickyConnections {
		for _, stats := range p.loadStatistics {
			if stats.matchesSourceHash {
				resultMap[stats.node.id] += formulaCoefficients[sourceIpHashMatch]
			}
		}
	}
	//max response time
	sort.SliceStable(p.loadStatistics, func(i, j int) bool {
		iResponseTimes := p.loadStatistics[i].responseTimes
		jResponseTimes := p.loadStatistics[j].responseTimes
		iMaxTime := p.findMaxTime(iResponseTimes)
		jMaxTime := p.findMaxTime(jResponseTimes)
		return iMaxTime > jMaxTime
	})
	for i, stats := range p.loadStatistics {
		resultMap[stats.node.id] += float32(i+1) * formulaCoefficients[lowestMaxResponseTime]
	}
	//avg response time
	sort.SliceStable(p.loadStatistics, func(i, j int) bool {
		iResponseTimes := p.loadStatistics[i].responseTimes
		jResponseTimes := p.loadStatistics[j].responseTimes
		iAvgTime := p.findAvgTime(iResponseTimes)
		jAvgTime := p.findAvgTime(jResponseTimes)
		return iAvgTime > jAvgTime
	})
	for i, stats := range p.loadStatistics {
		resultMap[stats.node.id] += float32(i+1) * formulaCoefficients[lowestAverageResponseTime]
	}
	//std dev in response time
	sort.SliceStable(p.loadStatistics, func(i, j int) bool {
		iResponseTimes := p.loadStatistics[i].responseTimes
		jResponseTimes := p.loadStatistics[j].responseTimes
		iStdDev := p.findStdDev(iResponseTimes)
		jStdDev := p.findStdDev(jResponseTimes)
		return iStdDev > jStdDev
	})
	for i, stats := range p.loadStatistics {
		resultMap[stats.node.id] += float32(i+1) * formulaCoefficients[lowestAverageDeviationInResponseTimes]
	}
	if p.shouldIncludeResourceInfo() {
		//cpu utilization
		sort.SliceStable(p.loadStatistics, func(i, j int) bool {
			return p.loadStatistics[i].usedResources.CpuUtilization > p.loadStatistics[j].usedResources.CpuUtilization
		})
		for i, stats := range p.loadStatistics {
			resultMap[stats.node.id] += float32(i+1) * formulaCoefficients[lowestCpuUtilization]
		}
		//free memory
		sort.SliceStable(p.loadStatistics, func(i, j int) bool {
			return p.loadStatistics[i].usedResources.FreeMemory < p.loadStatistics[j].usedResources.FreeMemory
		})
		for i, stats := range p.loadStatistics {
			resultMap[stats.node.id] += float32(i+1) * formulaCoefficients[highestFreeMemory]
		}
	}

	log.Println("node values:")
	for id, value := range resultMap {
		log.Printf("id: %d => val: %f\n", id, value)
	}

	winnerNodeId := p.getWinnerNodeId(resultMap)
	for _, stats := range p.loadStatistics {
		if stats.node.id == winnerNodeId {
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
		sum += math.Pow(float64(el)-mean, 2.0)
	}
	variance := sum / float64(n-1)
	return math.Sqrt(variance)
}

func (p *nodePicker) shouldIncludeResourceInfo() bool {
	for _, stats := range p.loadStatistics {
		if stats.usedResources.FreeMemory == 0 && stats.usedResources.CpuUtilization == 0 {
			return false
		}
	}
	return true
}

func (p *nodePicker) getWinnerNodeId(nodeValues map[uint]float32) uint {
	keys := make([]uint, 0, len(nodeValues))
	for k := range nodeValues {
		keys = append(keys, k)
	}
	sort.SliceStable(keys, func(i, j int) bool {
		return nodeValues[keys[i]] > nodeValues[keys[j]]
	})
	return keys[0]
}
