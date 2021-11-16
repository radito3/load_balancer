package main

import (
	"gopkg.in/yaml.v3"
	"load_balancer/balancing"
	"log"
	"os"
	"strconv"
)

func main() {
	if len(os.Args) != 3 {
		log.Fatalln("Invalid usage. Usage: load_balancer <port> <path to config file>")
	}

	log.SetPrefix("[load_balancer] [DEBUG] ")
	port, _ := strconv.Atoi(os.Args[1])
	bytes, err := os.ReadFile(os.Args[2])
	if err != nil {
		log.Fatalln(err)
	}
	var config balancing.Config
	err = yaml.Unmarshal(bytes, &config)
	if err != nil {
		log.Fatalln(err)
	}

	loadBalancer := balancing.NewLoadBalancer(config)
	log.Fatalln(loadBalancer.HandleTraffic(port))
}
