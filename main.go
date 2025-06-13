package main

import (
	"log"
	"os"
	"time"

	"github.com/galbarnahum/remoteSystemStatsMonitor/stats"
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

	// Start monitoring asynchronously
	if err := monitor.StartAsync(); err != nil {
		log.Fatalf("Failed to start monitoring: %v", err)
	}

	// Keep the program running until interrupted
}
