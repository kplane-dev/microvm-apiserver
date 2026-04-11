package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/kplane-dev/microvm-apiserver/cmd/microvm-apiserver/app"
)

func main() {
	var (
		listenAddr string
		agentURLs  string
	)

	flag.StringVar(&listenAddr, "listen", ":8443", "Listen address")
	flag.StringVar(&agentURLs, "agents", "", "Comma-separated firecracker-agent URLs (e.g., http://10.0.1.1:8090,http://10.0.1.2:8090)")
	flag.Parse()

	agents := strings.Split(agentURLs, ",")
	if agentURLs == "" {
		agents = nil
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()

	log.Printf("microvm-apiserver starting (agents=%v)", agents)
	if err := app.Run(ctx, agents, listenAddr); err != nil {
		log.Fatalf("server failed: %v", err)
		os.Exit(1)
	}
}
