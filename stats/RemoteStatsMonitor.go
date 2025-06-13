package stats

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// RemoteStatsMonitor monitors remote system stats at regular intervals
type RemoteStatsMonitor struct {
	collector   *remoteStatsCollector
	interval    time.Duration
	sampleDelta time.Duration // CPU sampling interval
	logger      *log.Logger
	logLineFunc func(*SystemStats) ([]byte, error)
	ctx         context.Context
	cancel      context.CancelFunc
	wg          sync.WaitGroup
	ctxMu       sync.Mutex // Protects context recreation
}

// NewRemoteStatsMonitorFromSFTP creates a new monitor from an existing SFTP client
func NewRemoteStatsMonitorFromSFTP(sftpClient *sftp.Client, interval time.Duration, sampleDelta time.Duration, logger *log.Logger) *RemoteStatsMonitor {
	collector := NewRemoteStatsCollectorFromSFTP(sftpClient, sampleDelta)
	ctx, cancel := context.WithCancel(context.Background())
	return &RemoteStatsMonitor{
		collector:   collector,
		interval:    interval,
		sampleDelta: sampleDelta,
		logger:      logger,
		logLineFunc: jsonLogLine, // Default log line function
		ctx:         ctx,
		cancel:      cancel,
	}
}

func jsonLogLine(stats *SystemStats) ([]byte, error) {
	data := SystemStatsToJSON(stats)
	data["timestamp"] = time.Now().Format("15:04:05.000000")
	//bytes, err := json.MarshalIndent(data, "", "  ")
	bytes, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}
	return bytes, nil
}

// NewRemoteStatsMonitorFromSSH creates a new monitor from an existing SSH client
func NewRemoteStatsMonitorFromSSH(sshClient *ssh.Client, interval time.Duration, sampleDelta time.Duration, logger *log.Logger) (*RemoteStatsMonitor, error) {
	collector, err := NewRemoteStatsCollectorFromSSH(sshClient, sampleDelta)
	if err != nil {
		return nil, fmt.Errorf("failed to create collector: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	return &RemoteStatsMonitor{
		collector:   collector,
		interval:    interval,
		sampleDelta: sampleDelta,
		logger:      logger,
		logLineFunc: jsonLogLine, // Default log line function
		ctx:         ctx,
		cancel:      cancel,
	}, nil
}

// NewRemoteStatsMonitorFromSSHConfig creates a new monitor from SSH configuration
func NewRemoteStatsMonitorFromSSHConfig(serverAddress string, config *ssh.ClientConfig, interval time.Duration, sampleDelta time.Duration, logger *log.Logger) (*RemoteStatsMonitor, error) {
	collector, err := NewRemoteStatsCollectorFromSSHConfig(serverAddress, config, sampleDelta)
	if err != nil {
		return nil, fmt.Errorf("failed to create collector: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	return &RemoteStatsMonitor{
		collector:   collector,
		interval:    interval,
		sampleDelta: sampleDelta,
		logger:      logger,
		logLineFunc: jsonLogLine, // Default log line function
		ctx:         ctx,
		cancel:      cancel,
	}, nil
}

// IsRunning returns whether the monitor is currently running
func (m *RemoteStatsMonitor) IsRunning() bool {
	m.ctxMu.Lock()
	defer m.ctxMu.Unlock()
	select {
	case <-m.ctx.Done():
		return false
	default:
		return true
	}
}

// ensureFreshContext creates a new context if the current one is cancelled
func (m *RemoteStatsMonitor) ensureFreshContext() {
	m.ctxMu.Lock()
	defer m.ctxMu.Unlock()

	select {
	case <-m.ctx.Done():
		// Context is cancelled, create a fresh one
		m.ctx, m.cancel = context.WithCancel(context.Background())
	default:
		// Context is still active, no need to recreate
	}
}

// collectAndLog collects stats and logs them using the configured logLine function
func (m *RemoteStatsMonitor) collectAndLog() error {
	stats, err := m.collector.GetSystemStats()
	if err != nil {
		return fmt.Errorf("failed to collect stats: %w", err)
	}

	// Use the configured logLine function to format the stats
	logData, err := m.logLineFunc(stats)
	if err != nil {
		return fmt.Errorf("failed to format log line: %w", err)
	}

	// Log the formatted data
	m.logger.Printf("%s", string(logData))

	return nil
}

// StartSync starts monitoring synchronously (blocking call)
func (m *RemoteStatsMonitor) StartSync() error {
	// Ensure we have a fresh context if the previous one was cancelled
	m.ensureFreshContext()

	m.wg.Add(1)
	defer m.wg.Done()

	ticker := time.NewTicker(m.interval)
	defer ticker.Stop()

	// Collect initial stats
	if err := m.collectAndLog(); err != nil {
		fmt.Printf("Error collecting initial stats: %v", err)
	}

	for {
		select {
		case <-m.ctx.Done():
			return nil
		case <-ticker.C:
			if err := m.collectAndLog(); err != nil {
				fmt.Printf("Error collecting stats: %v", err)
			}
		}
	}
}

// StartAsync starts monitoring asynchronously (non-blocking call)
func (m *RemoteStatsMonitor) StartAsync() error {
	// Ensure we have a fresh context if the previous one was cancelled
	m.ensureFreshContext()

	go func() {
		if err := m.StartSync(); err != nil {
			m.logger.Printf("Async monitoring stopped with error: %v", err)
		}
	}()

	return nil
}

// Stop stops the monitoring by cancelling the internal context
func (m *RemoteStatsMonitor) Stop() {
	m.ctxMu.Lock()
	cancel := m.cancel
	m.ctxMu.Unlock()

	cancel()
	m.wg.Wait() // Wait for monitoring to actually stop
}

// Close closes the underlying collector and stops monitoring
func (m *RemoteStatsMonitor) Close() error {
	m.Stop() // Safe to call multiple times due to sync.Once
	return m.collector.Close()
}

// GetCurrentStats gets the current system stats without logging
func (m *RemoteStatsMonitor) GetCurrentStats() (*SystemStats, error) {
	return m.collector.GetSystemStats()
}

// SetLogLine sets a custom log line formatting function
func (m *RemoteStatsMonitor) SetLogLineFunc(logLineFunc func(*SystemStats) ([]byte, error)) {
	m.logLineFunc = logLineFunc
}

// SetInterval updates the monitoring interval (only effective after restart)
func (m *RemoteStatsMonitor) SetInterval(interval time.Duration) {
	m.interval = interval
}

// GetInterval returns the current monitoring interval
func (m *RemoteStatsMonitor) GetInterval() time.Duration {
	return m.interval
}

// SetSampleDelta updates the CPU sampling interval (only effective after restart)
func (m *RemoteStatsMonitor) SetSampleDelta(sampleDelta time.Duration) {
	m.sampleDelta = sampleDelta
	m.collector.SetSampleDelta(sampleDelta)
}

// GetSampleDelta returns the current CPU sampling interval
func (m *RemoteStatsMonitor) GetSampleDelta() time.Duration {
	return m.sampleDelta
}

// SetLogFile sets the logger to write to the specified file
func (m *RemoteStatsMonitor) SetLogFile(filename string) error {
	file, err := os.OpenFile(filename, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to open log file: %w", err)
	}

	m.logger = log.New(file, "", 0)
	return nil
}
