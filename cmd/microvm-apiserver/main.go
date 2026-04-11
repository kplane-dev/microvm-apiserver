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
	flag.StringVar(&agentURLs, "agents", "", "Comma-separated firecracker-agent URLs")
	flag.Parse()

	var agents []string
	if agentURLs != "" {
		agents = strings.Split(agentURLs, ",")
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()

	server := app.NewServer(listenAddr, agents)
	log.Printf("microvm-apiserver starting (agents=%v)", agents)
	if err := server.Run(ctx); err != nil {
		log.Fatalf("server failed: %v", err)
		os.Exit(1)
	}
}
