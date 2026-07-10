package render

import (
	"fmt"
	"strings"

	"github.com/fatih/color"
)

var (
	Bold      = color.New(color.Bold)
	Dim       = color.New(color.Faint)
	GreenBold = color.New(color.FgGreen, color.Bold)
	CyanBold  = color.New(color.FgCyan, color.Bold)
	RedBold   = color.New(color.FgRed, color.Bold)
)

func FormatPctInt(current, previous int) string {
	if previous == 0 {
		if current == 0 {
			return "—"
		}
		return "new"
	}
	pct := float64(current-previous) / float64(previous) * 100
	sign := "+"
	if pct < 0 {
		sign = ""
	}
	return fmt.Sprintf("%s%.0f%%", sign, pct)
}

// PctColorInt is intentionally inverted: fewer distractions is better.
func PctColorInt(current, previous int) *color.Color {
	if previous == 0 || current == previous {
		return Dim
	}
	if current < previous {
		return GreenBold
	}
	return RedBold
}

func Sparkline(values []int) string {
	if len(values) == 0 {
		return ""
	}
	blocks := []rune{'▁', '▂', '▃', '▄', '▅', '▆', '▇', '█'}
	maxValue := 0
	for _, value := range values {
		if value > maxValue {
			maxValue = value
		}
	}
	if maxValue == 0 {
		return strings.Repeat(" ", len(values))
	}
	var b strings.Builder
	for _, value := range values {
		if value == 0 {
			b.WriteRune(' ')
			continue
		}
		index := int(float64(value) / float64(maxValue) * float64(len(blocks)))
		if index >= len(blocks) {
			index = len(blocks) - 1
		}
		b.WriteRune(blocks[index])
	}
	return b.String()
}

func VerticalBars(values []int, labels []string, c *color.Color) {
	maxValue := 0
	for _, value := range values {
		if value > maxValue {
			maxValue = value
		}
	}
	if maxValue == 0 {
		fmt.Println("  (none)")
		return
	}
	chartHeight := min(8, maxValue)
	columnWidth := 7
	for _, label := range labels {
		columnWidth = max(columnWidth, len(label)+1)
	}
	for row := chartHeight; row >= 1; row-- {
		threshold := float64(row) / float64(chartHeight) * float64(maxValue)
		fmt.Print("  ")
		for _, value := range values {
			if value > 0 && float64(value) >= threshold {
				fmt.Printf("%s%s", c.Sprint("██"), strings.Repeat(" ", columnWidth-2))
			} else {
				fmt.Print(strings.Repeat(" ", columnWidth))
			}
		}
		fmt.Println()
	}
	fmt.Print("  ")
	for _, label := range labels {
		fmt.Printf("%-*s", columnWidth, label)
	}
	fmt.Println()
	fmt.Print("  ")
	for _, value := range values {
		Dim.Printf("%-*d", columnWidth, value)
	}
	fmt.Println()
}
