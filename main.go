package main

import (
	"context"
	"log"
	"os"
	"time"

	"github.com/galbarnahum/h2loadGo/remoteSystemStatsMonitor/stats"
	"golang.org/x/crypto/ssh"
)

func main() {
	// Create SSH client config
	config := &ssh.ClientConfig{
		User: "gal",
		Auth: []ssh.AuthMethod{
			ssh.Password("gal"),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}

	// Create a logger that writes to stdout
	logger := log.New(os.Stdout, "", 0)

	// Create the monitor with 1 second interval and 100ms CPU sampling
	monitor, err := stats.NewRemoteStatsMonitorFromSSHConfig("192.168.205.131:22", config, time.Second, 300*time.Millisecond, logger)
	if err != nil {
		log.Fatalf("Failed to create monitor: %v", err)
	}
	defer monitor.Close()

	// Create a context that we can use to stop the monitoring
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start monitoring asynchronously
	if err := monitor.StartAsync(ctx); err != nil {
		log.Fatalf("Failed to start monitoring: %v", err)
	}

	// Keep the program running until interrupted
	select {
	case <-ctx.Done():
		log.Println("Monitoring stopped")
	}
}
