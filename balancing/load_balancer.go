package balancing

import (
	"encoding/json"
	"io"
	"load_balancer/util"
	"log"
	"net"
	"strconv"
	"strings"
	"time"
)

type LoadBalancer struct {
	serviceName                          string
	useStickyConnections                 bool
	resourceMonitoringAgentQueryInterval time.Duration
	nodes                                []node
	connections                          *util.ConcurrentUintMap
	sourceToDestinationHashMap           *util.ConcurrentStringMap
	responseTimes                        *util.ConcurrentUintMap
	nodeResources                        *util.ConcurrentUintMap
}

type resources struct {
	CpuUtilization uint8  `json:"cpu"`
	FreeMemory     uint64 `json:"memory"`
}

type LoadStatistics struct {
	node              node
	connections       uint
	matchesSourceHash bool
	responseTimes     []time.Duration
	usedResources     resources
}

type Config struct {
	ServiceName                          string        `yaml:"serviceName"`
	StickyConnections                    bool          `yaml:"stickyConnections"`
	resourceMonitoringAgentQueryInterval time.Duration `yaml:"resource_monitoring_agent_query_interval"`
	Nodes                                []NodeInfo    `yaml:"nodes"`
}

type NodeInfo struct {
	Address                  string `yaml:"address"`
	ResourceMonitorAgentPort uint   `yaml:"resourceMonitorAgentPort"`
	MaxConnections           int    `yaml:"maxConnections"` //a hypothetical feature could be if "maxConns" is negative, do not use node
}

func NewLoadBalancer(config Config) *LoadBalancer {
	return &LoadBalancer{
		serviceName:                          config.ServiceName,
		useStickyConnections:                 config.StickyConnections,
		resourceMonitoringAgentQueryInterval: config.resourceMonitoringAgentQueryInterval,
		nodes:                                createNodes(config.Nodes...),
		connections:                          util.NewUintMap(),
		sourceToDestinationHashMap:           util.NewStringMap(),
		responseTimes:                        util.NewUintMap(),
		nodeResources:                        util.NewUintMap(),
	}
}

func createNodes(nodes ...NodeInfo) []node {
	if len(nodes) == 0 {
		panic("no addresses passed to load balancer")
	}
	var result []node
	var nodeId uint
	for _, nodeInfo := range nodes {
		parts := strings.Split(nodeInfo.Address, ":")
		port, _ := strconv.Atoi(parts[1])
		result = append(result, node{
			id:                  nodeId,
			address:             parts[0],
			port:                port,
			maxConnections:      nodeInfo.MaxConnections,
			resourceMonitorPort: nodeInfo.ResourceMonitorAgentPort,
		})
		nodeId++
	}
	return result
}

type node struct {
	id                  uint
	address             string
	port                int
	maxConnections      int
	resourceMonitorPort uint //realistically this could be a different IP address
}

func (n node) addressToString() string {
	return n.address + ":" + strconv.Itoa(n.port)
}

func (lb *LoadBalancer) HandleTraffic(port int) error {
	for _, node := range lb.nodes {
		go lb.pollResourceMonitoringAgent(node)
	}

	address := &net.TCPAddr{
		IP:   net.ParseIP("127.0.0.1"),
		Port: port,
	}
	listener, err := net.ListenTCP("tcp", address)
	if err != nil {
		return err
	}
	defer listener.Close()

	for {
		conn, err := listener.Accept()
		if err != nil {
			return err
		}
		err = lb.handleTcpConn(conn)
		if err != nil {
			return err
		}
	}
}

func (lb *LoadBalancer) pollResourceMonitoringAgent(node node) {
	address := node.address + ":" + strconv.Itoa(int(node.resourceMonitorPort))
	addr, _ := net.ResolveUDPAddr("udp", address)
	for {
		log.Printf("calling %s for a resource summary...\n", addr.String())
		conn, err := net.DialUDP("udp", nil, addr)
		if err != nil {
			log.Println(err)
			lb.nodeResources.Put(node.id, resources{})
			time.Sleep(lb.resourceMonitoringAgentQueryInterval)
			continue
		}
		res := lb.readUsedResources(conn)
		log.Printf("resources from %s: %v\n", addr.String(), res)
		lb.nodeResources.Put(node.id, res)
		conn.Close()
		time.Sleep(lb.resourceMonitoringAgentQueryInterval)
	}
}

func (lb *LoadBalancer) readUsedResources(conn *net.UDPConn) resources {
	_, err := conn.Write([]byte("connect"))
	if err != nil {
		log.Println(err)
		return resources{}
	}

	buff := make([]byte, 1024)
	n, _, err := conn.ReadFromUDP(buff)
	return lb.parseResourcesFromJson(buff[:n])
}

func (lb *LoadBalancer) parseResourcesFromJson(data []byte) resources {
	var result resources
	err := json.Unmarshal(data, &result)
	if err != nil {
		return resources{}
	}
	return result
}

func (lb *LoadBalancer) handleTcpConn(conn net.Conn) error {
	defer conn.Close()

	remoteAddress := conn.RemoteAddr().String()
	sourceIpHash := util.Hash(strings.Split(remoteAddress, ":")[0])

	node := lb.pickNode(sourceIpHash)
	forwardingConn, err := net.Dial("tcp", node.addressToString())
	if err != nil {
		return err
	}
	defer forwardingConn.Close()

	lb.incrementNodeConnections(node.id)
	defer lb.decrementNodeConnections(node.id)

	if lb.useStickyConnections {
		lb.sourceToDestinationHashMap.Put(sourceIpHash, node)
	}

	_, err = io.Copy(forwardingConn, conn)
	if err != nil {
		return err
	}

	start := time.Now()
	_, err = io.Copy(conn, forwardingConn)
	if err != nil {
		return err
	}
	elapsed := time.Since(start)
	lb.addResponseTime(node.id, elapsed)
	return nil
}

func (lb *LoadBalancer) incrementNodeConnections(nodeId uint) {
	conns, present := lb.connections.Get(nodeId)
	if present {
		lb.connections.Put(nodeId, conns.(uint) + 1)
	} else {
		lb.connections.Put(nodeId, uint(1))
	}
}

func (lb *LoadBalancer) decrementNodeConnections(nodeId uint) {
	conns, present := lb.connections.Get(nodeId)
	if present && conns.(uint) > 0 {
		lb.connections.Put(nodeId, conns.(uint) - 1)
	}
}

func (lb *LoadBalancer) addResponseTime(nodeId uint, responseTime time.Duration) {
	times, present := lb.responseTimes.Get(nodeId)
	if !present {
		times = []time.Duration{}
	}
	if len(times.([]time.Duration)) >= 20 {
		times = append([]time.Duration{}, times.([]time.Duration)[1:]...)
	}
	times = append(times.([]time.Duration), responseTime)
	lb.responseTimes.Put(nodeId, times)
}

/*
Weighted least connections (keep connection counter) (30%)
Source IP hash (keep map of source to destination addresses) (lowest priority) (15%)
Response time (keep map of last 20 response times and compute a value for max (8%), avg (most priority) (13%) & std dev (4%)) (25%)
Resource adaptive (call RMA for a json with the cpu & memory stats) (30%)
(RMA - resource monitoring agent)

-------
Maybe:
after a node's value is computed, a negative coefficient may be applied (value so far is multiplied by this)
if the memory and latency values have been progressively getting worse (over the past few computations)

to check for potential memory leaks
if ~80% of last free memory values are only decreasing -> that could be a potential memory leak

to check latency degradation
calculate the function graph (an aggregate of the values)

e.g.
    *       *
  *   *   *
*       *

then try to find the aggregate/common/coalescing line / singularity (this goes a bit into ML maybe)

e.g.
    *       *
--*---*---*--
*       *

in this example, the negative coefficient is 0 because the line is straight
the more slanted upwards it is, the higher the negative coefficient
in other words, the coefficient could be equal to the degree of the line in relation to a horizontal line
(i.e. if the line is 0deg - perfect, 45deg - quite bad, 60deg - worse, and so on)
(a 90deg angle is impossible because we are essentially calculating the tangent of the angle and
	a tangent of 90deg is infinity)
(e.g. an awful scenario would be an exponential progression)

         *
         *
        *
       *
  *   *
*   *

this may mean that the server may not be operating well (it could also be because of a router/switch on the network)
-------

top level formula could look something like this:
percent of available connections * 0.25 (if max_conns == 30 and there are 15 active conns, this would be 50%)
+
(if config.stickyConnections is true) 0.15 if hash(client.address) matches current node else 0
+
max response time * 0.08
+
average response time * 0.13
+
average deviation * 0.04
+
CPU utilization * 0.20
+
highest free memory order index * 0.15 (in other words, sort the amount of remaining memory per node
	in descending order and take the node's index from that array)
=
total value
=> pick max out of nodes
*/

func (lb *LoadBalancer) pickNode(sourceIpHash string) node {
	var loadStats []LoadStatistics
	for _, currentNode := range lb.nodes {
		matchedNode, presentSourceHash := lb.sourceToDestinationHashMap.Get(sourceIpHash)
		conns, presentConns := lb.connections.Get(currentNode.id)
		if !presentConns {
			conns = uint(0)
		}
		resTimes, presentResTimes := lb.responseTimes.Get(currentNode.id)
		if !presentResTimes {
			resTimes = []time.Duration{}
		}
		nodeResources, presentResources := lb.nodeResources.Get(currentNode.id)
		if !presentResources {
			nodeResources = resources{}
		}
		stats := LoadStatistics{
			node:              currentNode,
			connections:       conns.(uint),
			matchesSourceHash: presentSourceHash && matchedNode.(node).id == currentNode.id,
			responseTimes:     resTimes.([]time.Duration),
			usedResources:     nodeResources.(resources),
		}
		loadStats = append(loadStats, stats)
	}

	picker := &nodePicker{
		stickyConnections: lb.useStickyConnections,
		loadStatistics:    loadStats,
	}
	return picker.Pick()
}
