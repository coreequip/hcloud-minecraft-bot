package main

import (
	"fmt"
	"strings"
	"time"
)

type ProgressBar struct {
	Total     int
	Current   int
	Width     int
	StartTime time.Time
}

func NewProgressBar(total int) *ProgressBar {
	return &ProgressBar{
		Total:     total,
		Current:   0,
		Width:     20,
		StartTime: time.Now(),
	}
}

func (pb *ProgressBar) Update(current int) {
	pb.Current = current
}

func (pb *ProgressBar) String() string {
	if pb.Total == 0 {
		return "`████████████████████` 100%"
	}

	percentage := float64(pb.Current) / float64(pb.Total) * 100
	filled := int(float64(pb.Width) * float64(pb.Current) / float64(pb.Total))

	bar := strings.Repeat("█", filled) + strings.Repeat("░", pb.Width-filled)

	return fmt.Sprintf("`%s` %3.0f%%", bar, percentage)
}

func (pb *ProgressBar) TimeRemaining() string {
	if pb.Current == 0 {
		return "berechne..."
	}

	if pb.Current >= 100 {
		return "fertig"
	}

	elapsed := time.Since(pb.StartTime)

	// Simple calculation based on current progress
	estimatedTotal := time.Duration(float64(elapsed) * 100.0 / float64(pb.Current))
	remaining := estimatedTotal - elapsed

	// If estimate is more than 5 minutes or progress is low, it's unreliable
	if remaining > 5*time.Minute || pb.Current < 10 {
		return "berechne...⏳"
	}

	if remaining < 0 {
		return "fast fertig"
	}

	return formatDuration(remaining)
}

func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%d Sekunden", int(d.Seconds()))
	} else if d < time.Hour {
		minutes := int(d.Minutes())
		seconds := int(d.Seconds()) % 60
		if seconds > 30 {
			minutes++ // Round up
		}
		return fmt.Sprintf("%d Minuten", minutes)
	}
	return fmt.Sprintf("%.1f Stunden", d.Hours())
}

func formatProgressMessage(action string, pb *ProgressBar, status string) string {
	return fmt.Sprintf("*%s*\n\n%s\n\n⏱ Restzeit: %s\n\n_%s_",
		action,
		pb.String(),
		pb.TimeRemaining(),
		status,
	)
}
