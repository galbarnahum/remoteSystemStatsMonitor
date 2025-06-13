package stats

import (
	"bufio"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

type CPUStat struct {
	Core     string // e.g., "cpu0", "cpu1"
	UsagePct float64
}

type SystemStats struct {
	TotalMemoryMB      float64
	UsedMemoryMB       float64
	UsedMemoryPercent  float64
	TotalCPUPercentage float64   // "cpu" aggregate line
	CPUStats           []CPUStat // only "cpu0", "cpu1", ...
}

// remoteStatsCollector handles collecting system stats from a remote system via SFTP
type remoteStatsCollector struct {
	sftpClient     *sftp.Client
	sshClient      *ssh.Client
	sampleDelta    time.Duration
	ownsSftpClient bool // true if we created the SFTP client and should close it
	ownsSSHClient  bool // true if we created the SSH client and should close it
}

// NewRemoteStatsCollectorFromSFTP creates a new instance of remoteStatsCollector from an existing SFTP client
func NewRemoteStatsCollectorFromSFTP(sftpClient *sftp.Client, sampleDelta time.Duration) *remoteStatsCollector {
	return &remoteStatsCollector{
		sftpClient:     sftpClient,
		sampleDelta:    sampleDelta,
		ownsSftpClient: false,
		ownsSSHClient:  false,
	}
}

// NewRemoteStatsCollectorFromSSH creates a new instance of remoteStatsCollector from an SSH connection
func NewRemoteStatsCollectorFromSSH(sshClient *ssh.Client, sampleDelta time.Duration) (*remoteStatsCollector, error) {
	sftpClient, err := sftp.NewClient(sshClient)
	if err != nil {
		return nil, fmt.Errorf("failed to create SFTP client: %w", err)
	}
	return NewRemoteStatsCollectorFromSFTP(sftpClient, sampleDelta), nil
}

// NewRemoteStatsCollectorFromSSHConfig creates a new instance of remoteStatsCollector from SSH configuration
func NewRemoteStatsCollectorFromSSHConfig(serverAddress string, config *ssh.ClientConfig, sampleDelta time.Duration) (*remoteStatsCollector, error) {
	// Connect to SSH server
	sshClient, err := ssh.Dial("tcp", serverAddress, config)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to SSH server: %w", err)
	}
	collector, err := NewRemoteStatsCollectorFromSSH(sshClient, sampleDelta)
	if err != nil {
		return nil, fmt.Errorf("failed to create remote stats collector: %w", err)
	}
	collector.ownsSSHClient = true
	return collector, nil
}

// Close closes the SFTP and SSH clients if we own them
func (r *remoteStatsCollector) Close() error {
	var err error

	if r.ownsSftpClient && r.sftpClient != nil {
		if closeErr := r.sftpClient.Close(); closeErr != nil {
			err = closeErr
		}
	}

	if r.ownsSSHClient && r.sshClient != nil {
		if closeErr := r.sshClient.Close(); closeErr != nil {
			err = closeErr
		}
	}

	return err
}

// SetSampleDelta updates the CPU sampling interval
func (r *remoteStatsCollector) SetSampleDelta(sampleDelta time.Duration) {
	r.sampleDelta = sampleDelta
}

// GetSampleDelta returns the current CPU sampling interval
func (r *remoteStatsCollector) GetSampleDelta() time.Duration {
	return r.sampleDelta
}

func (r *remoteStatsCollector) getMemoryStats() (totalMB float64, usedMB float64, err error) {
	file, err := r.sftpClient.Open("/proc/meminfo")
	if err != nil {
		return
	}
	defer file.Close()

	var total, available float64
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		key := fields[0]
		val, err2 := strconv.ParseFloat(fields[1], 64)
		if err2 != nil {
			continue
		}
		switch key {
		case "MemTotal:":
			total = val
		case "MemAvailable:":
			available = val
		}
	}
	if err = scanner.Err(); err != nil {
		return
	}
	if total == 0 {
		err = fmt.Errorf("invalid meminfo (MemTotal is zero)")
		return
	}
	totalMB = total / 1024
	usedMB = (total - available) / 1024
	return
}

func (r *remoteStatsCollector) getCPUStats() (totalUsage float64, perCore []CPUStat, err error) {
	snapshot := func() (map[string][]float64, error) {
		file, err := r.sftpClient.Open("/proc/stat")
		if err != nil {
			return nil, err
		}
		defer file.Close()

		stats := make(map[string][]float64)
		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			line := scanner.Text()
			if !strings.HasPrefix(line, "cpu") {
				break
			}
			fields := strings.Fields(line)
			core := fields[0]
			values := make([]float64, 0, len(fields)-1)
			for _, f := range fields[1:] {
				v, err := strconv.ParseFloat(f, 64)
				if err != nil {
					return nil, fmt.Errorf("failed to parse CPU stat: %w", err)
				}
				values = append(values, v)
			}
			stats[core] = values
		}
		return stats, scanner.Err()
	}

	stat1, err := snapshot()
	if err != nil {
		return
	}
	time.Sleep(r.sampleDelta)
	stat2, err := snapshot()
	if err != nil {
		return
	}

	for core, values1 := range stat1 {
		values2, ok := stat2[core]
		if !ok {
			continue
		}
		var total1, total2, idle1, idle2 float64
		for i := range values1 {
			total1 += values1[i]
			total2 += values2[i]
		}
		if len(values1) > 3 {
			idle1 = values1[3]
			idle2 = values2[3]
		} else {
			continue
		}
		deltaIdle := idle2 - idle1
		deltaTotal := total2 - total1
		if deltaTotal == 0 {
			continue
		}
		usage := (1 - deltaIdle/deltaTotal) * 100.0

		if core == "cpu" {
			totalUsage = usage
		} else {
			perCore = append(perCore, CPUStat{Core: core, UsagePct: usage})
		}
	}

	return
}

func (r *remoteStatsCollector) GetSystemStats() (*SystemStats, error) {
	totalMem, usedMem, err := r.getMemoryStats()
	if err != nil {
		return nil, fmt.Errorf("failed to get memory stats: %w", err)
	}

	totalCPU, coreStats, err := r.getCPUStats()
	if err != nil {
		return nil, fmt.Errorf("failed to get CPU stats: %w", err)
	}

	return &SystemStats{
		TotalMemoryMB:      totalMem,
		UsedMemoryMB:       usedMem,
		UsedMemoryPercent:  (usedMem / totalMem) * 100.0,
		TotalCPUPercentage: totalCPU,
		CPUStats:           coreStats,
	}, nil
}
