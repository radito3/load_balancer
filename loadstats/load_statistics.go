package loadstats

import (
	"encoding/json"
	"fmt"
	"load_balancer/util"
	"net"
	"time"
)

type LoadStatistics struct {
	connections    uint
	sourceAddrHash string
	responseTimes  []time.Duration
	usedResources  resources
}

type resources struct {
	CpuUtilization uint8  `json:"cpu"`
	FreeMemory     uint64 `json:"memory"`
}

var defaultResources resources

func CreateLoadStatistics(conns uint, sourceAddr string, responseTimes []time.Duration, address string) (LoadStatistics, error) {
	sourceAddrHash := util.Hash(sourceAddr)
	addr, _ := net.ResolveUDPAddr("udp", address)
	conn, err := net.DialUDP("udp", nil, addr)
	if err != nil {
		return LoadStatistics{}, err
	}
	defer conn.Close()
	return LoadStatistics{
		connections:    conns,
		sourceAddrHash: sourceAddrHash,
		responseTimes:  responseTimes,
		usedResources:  readUsedResources(conn),
	}, nil
}

func readUsedResources(conn *net.UDPConn) resources {
	_, err := conn.Write([]byte("connect"))
	if err != nil {
		fmt.Println(err)
		return defaultResources
	}

	buff := make([]byte, 1024)
	n, _, err := conn.ReadFromUDP(buff)
	buff = buff[:n]
	return parseResourcesFromJson(buff)
}

func parseResourcesFromJson(data []byte) resources {
	var result resources
	err := json.Unmarshal(data, &result)
	if err != nil {
		return defaultResources
	}
	return result
}
