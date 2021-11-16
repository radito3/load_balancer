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

type formulaElement int

const (
	percentAvailConnections formulaElement = iota
	sourceIpHashMatch
	maxResponseTime
	averageResponseTime
	averageDeviationInResponseTimes
	cpuUtilization
	highestFreeMemory
)

var formulaCoefficients map[formulaElement]float32

func init() {
	formulaCoefficients = map[formulaElement]float32{
		percentAvailConnections:         float32(0.25),
		sourceIpHashMatch:               float32(0.15),  //maybe this needs to be a higher coefficient or just 90%
		maxResponseTime:                 float32(-0.08),
		averageResponseTime:             float32(-0.13),
		averageDeviationInResponseTimes: float32(-0.04),
		cpuUtilization:                  float32(-0.20),
		highestFreeMemory:               float32(0.15),
	}
}

//Pick select node with the highest value based on following formula:
// percent of available connections * 0.25 (if max_conns == 30 and there are 15 active conns, this would be 50%)
// +
// (if config.stickyConnections is true) 0.15 if hash(client.address) matches current node else 0
// +
// max response time * -0.08
// +
// average response time * -0.13
// +
// average deviation in response times * -0.04
// +
// CPU utilization * -0.20
// +
// highest free memory order index * 0.15
func (p *nodePicker) Pick() node {
	resultMap := make(map[uint]float32, len(p.loadStatistics))
	//percent available connections
	for _, stats := range p.loadStatistics {
		maxConns := stats.node.maxConnections
		percentAvailConns := float32(int(stats.connections)/maxConns) * float32(100)
		resultMap[stats.node.id] += percentAvailConns * formulaCoefficients[percentAvailConnections]
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
	for _, stats := range p.loadStatistics {
		maxRespTime := p.findMaxTime(stats.responseTimes)
		resultMap[stats.node.id] += float32(maxRespTime.Milliseconds()) * formulaCoefficients[maxResponseTime]
	}
	//avg response time
	for _, stats := range p.loadStatistics {
		avgResponseTime := p.findAvgTime(stats.responseTimes)
		resultMap[stats.node.id] += avgResponseTime * formulaCoefficients[averageResponseTime]
	}
	//std dev in response time
	for _, stats := range p.loadStatistics {
		stdDev := p.findStdDev(stats.responseTimes)
		resultMap[stats.node.id] += stdDev * formulaCoefficients[averageDeviationInResponseTimes]
	}
	if p.shouldIncludeResourceInfo() {
		//cpu utilization
		for _, stats := range p.loadStatistics {
			cpuUtilPercent := stats.usedResources.CpuUtilization
			resultMap[stats.node.id] += float32(cpuUtilPercent) * formulaCoefficients[cpuUtilization]
		}
		//free memory
		sort.Slice(p.loadStatistics, func(i, j int) bool {
			return p.loadStatistics[i].usedResources.FreeMemory < p.loadStatistics[j].usedResources.FreeMemory
		})
		for i, stats := range p.loadStatistics {
			resultMap[stats.node.id] += float32(i+1) * formulaCoefficients[highestFreeMemory]
		}
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

func (p *nodePicker) findAvgTime(arr []time.Duration) float32 {
	if len(arr) == 0 {
		return 0
	}
	var sum int64
	for _, el := range arr {
		sum += el.Milliseconds()
	}
	return float32(sum) / float32(len(arr))
}

func (p *nodePicker) findStdDev(arr []time.Duration) float32 {
	n := len(arr)
	if n < 2 {
		return 0
	}
	mean := p.findAvgTime(arr)
	var sum float64
	for _, el := range arr {
		sum += math.Pow(float64(el.Milliseconds())-float64(mean), 2.0)
	}
	variance := sum / float64(n-1)
	return float32(math.Sqrt(variance))
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
