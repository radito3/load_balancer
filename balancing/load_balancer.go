package balancing

import (
	"io"
	"load_balancer/loadstats"
	"load_balancer/util"
	"net"
	"strconv"
	"strings"
	"time"
)

type LoadBalancer struct {
	serviceName                string
	stickyConnections          bool
	nodes                      []node
	connections                map[uint]uint
	sourceToDestinationHashMap map[string]node
	responseTimes              map[uint][]time.Duration
}

type Config struct {
	ServiceName       string     `yaml:"serviceName"`
	StickyConnections bool       `yaml:"stickyConnections"`
	Nodes             []NodeInfo `yaml:"nodes"`
}

type NodeInfo struct {
	Address                  string `yaml:"address"`
	ResourceMonitorAgentPort uint   `yaml:"resourceMonitorAgentPort"`
	MaxConnections           int    `yaml:"maxConnections"` //a hypothetical feature could be if "maxConns" is negative, do not use node
}

func NewLoadBalancer(config Config) *LoadBalancer {
	return &LoadBalancer{
		serviceName:                config.ServiceName,
		stickyConnections:          config.StickyConnections,
		nodes:                      createNodes(config.Nodes...),
		connections:                make(map[uint]uint),
		sourceToDestinationHashMap: make(map[string]node),
		responseTimes:              make(map[uint][]time.Duration),
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

func (lb *LoadBalancer) handleTcpConn(conn net.Conn) error {
	defer conn.Close()

	node := lb.pickNode()
	forwardingConn, err := net.Dial("tcp", node.addressToString())
	if err != nil {
		return err
	}
	defer forwardingConn.Close()

	lb.connections[node.id]++
	defer lb.decrementNodeConnections(node.id)

	remoteAddress := conn.RemoteAddr().String()
	lb.sourceToDestinationHashMap[util.Hash(strings.Split(remoteAddress, ":")[0])] = node

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

func (lb *LoadBalancer) decrementNodeConnections(nodeId uint) {
	conns := lb.connections[nodeId]
	if conns > 0 {
		lb.connections[nodeId]--
	}
}

func (lb *LoadBalancer) addResponseTime(nodeId uint, responseTime time.Duration) {
	times := lb.responseTimes[nodeId]
	if len(times) >= 20 {
		newTimes := make([]time.Duration, 19)
		copy(newTimes, times[1:]) //FIXME this doesn't work, there is still a default-value element at 20th position
		times = newTimes
	}
	times = append(times, responseTime)
	lb.responseTimes[nodeId] = times
}

/*
Weighted least connections (keep connection counter) (30%)
Source IP hash (keep map of source to destination addresses) (lowest priority) (15%)
Response time (keep map of last 20 response times and compute a value for max (8%), avg (most priority) (13%) & std dev (4%)) (25%)
Resource adaptive (call 127.0.0.1:81 on UDP for a json with the cpu & memory stats) (30%)
= 100% of positive coefficients

figure out coefficients for these 4 values
compute sum for each server node
pick best
route incoming connection/traffic to winner

-------
Maybe:
after a node's value is computed, a negative coefficient may be applied (value so far is multiplied by this)
if the memory and latency values have been progressively getting worse (over the past few computations)

to check progression
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
least connections out of all nodes order index * 0.15 (in other words, sort the amount of remaining connections per node
	in descending order and take the node's index from that array)
+
percent of available connections * 0.15 (if max_conns == 30 and there are 15 active conns, this would be 50%)
+
(if config.stickyConnections is true) 0.15 if hash(client.address) matches current node else 0
+
lowest max response time order index * 0.08
+
lowest average response time order index * 0.13
+
lowest average deviation order index * 0.04
+
lowest CPU utilization order index * 0.15
+
highest free memory order index * 0.15
=
total value
=> pick max out of nodes
*/

func (lb *LoadBalancer) pickNode() node {
	var loadStats []loadstats.LoadStatistics
	for _, node := range lb.nodes {
		stats, err := loadstats.CreateLoadStatistics(
			lb.connections[node.id],
			node.address,
			lb.responseTimes[node.id],
			node.addressToString(),
		)
		if err != nil {
			//TODO how to handle this error?
		}
		loadStats = append(loadStats, stats)
	}

	picker := nodePicker{
		stickyConnections: lb.stickyConnections,
		nodes:             lb.nodes,
		loadStatistics:    loadStats,
	}
	return picker.Pick()
}
