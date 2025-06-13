package stats

import (
	"fmt"
	"sort"
)

func PrintSystemStats(stats *SystemStats) {
	fmt.Println("📊 System Stats Summary")
	fmt.Println("───────────────────────────────")
	fmt.Printf("🧠 Memory Used: %.2f MB / %.2f MB (%.2f%%)\n",
		stats.UsedMemoryMB, stats.TotalMemoryMB, stats.UsedMemoryPercent)

	fmt.Printf("⚙️  Total CPU Usage: %.2f%%\n", stats.TotalCPUPercentage)

	if len(stats.CPUStats) > 0 {
		fmt.Println("🔧 Per-Core CPU Usage:")
		// Sort by core name (cpu0, cpu1, ...)
		sort.Slice(stats.CPUStats, func(i, j int) bool {
			return stats.CPUStats[i].Core < stats.CPUStats[j].Core
		})
		for _, cpu := range stats.CPUStats {
			fmt.Printf("   • %-5s: %.2f%%\n", cpu.Core, cpu.UsagePct)
		}
	}
	fmt.Println("───────────────────────────────")
}

func SystemStatsToJSON(stats *SystemStats) map[string]any {
	data := map[string]any{
		"total_memory_mb":      stats.TotalMemoryMB,
		"used_memory_mb":       stats.UsedMemoryMB,
		"used_memory_percent":  stats.UsedMemoryPercent,
		"total_cpu_percentage": stats.TotalCPUPercentage,
		"per_core_cpu_percentages": func() map[string]float64 {
			m := make(map[string]float64)
			for _, cpu := range stats.CPUStats {
				m[cpu.Core] = cpu.UsagePct
			}
			return m
		}(),
	}
	return data
}
