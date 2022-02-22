package main

import (
	"fmt"
	go_failover "github.com/jbain/go-failover"
	"log"
	"os"
	"strconv"
	"strings"
	"time"
)

func main() {

	args := os.Args
	if len(args) < 4 {
		fmt.Printf("Usage:\n  ./%s <id> <groupid> <port> [peerid1,peerid2]")
	}
	groupdId := args[2]
	port, err := strconv.Atoi(args[3])
	if err != nil {
		fmt.Printf("Port must be a number between 0-65535\n")
		os.Exit(1)
	}
	nodeId := fmt.Sprintf("%s:%d", args[1], port)
	peerIds := make([]string, 0)
	if len(args) >= 5 {
		peerIds = strings.Split(args[4], ",")
	}
	log.Printf("Peers %v", peerIds)

	t := go_failover.NewHttpTransport("", uint16(port))
	t.Init()

	go_failover.New(
		groupdId,
		nodeId,
		go_failover.DefaultPriority,
		3000,
		t,
		func() {
			log.Printf("%s Active\n", nodeId)
		},
		func() {
			log.Printf("%s Standby\n", nodeId)
		}, peerIds...)

	time.Sleep(600 * time.Second)
}
