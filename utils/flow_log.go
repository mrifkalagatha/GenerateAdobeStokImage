package utils

import (
	"fmt"
	"log"
	"strings"
	"time"
)

const (
	ansiReset = "\x1b[0m"
	ansiDim   = "\x1b[2m"
	ansiCyan  = "\x1b[36m"
	ansiGreen = "\x1b[32m"
	ansiRed   = "\x1b[31m"
	ansiBlue  = "\x1b[34m"
	ansiMag   = "\x1b[35m"
)

func LogFlow(logger *log.Logger, icon, stage, format string, args ...any) {
	if logger == nil {
		return
	}

	color := levelColor(icon)
	header := fmt.Sprintf("%s╔═ %s %s%s", color, emojiIcon(icon), strings.ToUpper(stage), ansiReset)
	meta := fmt.Sprintf("%s║ @root/pipeline :: %s%s", ansiDim, time.Now().Format("2006-01-02 15:04:05.000"), ansiReset)
	body := indentMessage(fmt.Sprintf(format, args...))
	footer := fmt.Sprintf("%s╚════════════════════════════════════════════════════════════%s", color, ansiReset)

	logger.Printf("\n%s\n%s\n%s\n%s", header, meta, body, footer)
}

func LogError(logger *log.Logger, stage, format string, args ...any) {
	LogFlow(logger, "[ERR]", stage, format, args...)
}

func LogSuccess(logger *log.Logger, stage, format string, args ...any) {
	LogFlow(logger, "[OK]", stage, format, args...)
}

func indentMessage(msg string) string {
	lines := strings.Split(msg, "\n")
	for i := range lines {
		lines[i] = "║  ├─ " + lines[i]
	}
	return strings.Join(lines, "\n")
}

func levelColor(icon string) string {
	switch icon {
	case "[ERR]":
		return ansiRed
	case "[OK]":
		return ansiGreen
	case "[GPT]", "[AI]", "[PRM]":
		return ansiMag
	case "[REQ]", "[VID]", "[WRK]":
		return ansiBlue
	default:
		return ansiCyan
	}
}

func emojiIcon(icon string) string {
	switch icon {
	case "[REQ]":
		return "📡"
	case "[SYS]":
		return "🧰"
	case "[WRK]":
		return "🧵"
	case "[JOB]":
		return "🆔"
	case "[PRM]":
		return "🧪"
	case "[IMG]":
		return "🖼️"
	case "[AI]":
		return "🤖"
	case "[UPS]":
		return "🚀"
	case "[VID]":
		return "🎞️"
	case "[IO]":
		return "💾"
	case "[ZIP]":
		return "🗜️"
	case "[GPT]":
		return "💬"
	case "[OK]":
		return "✅"
	case "[ERR]":
		return "❌"
	default:
		return "🟢"
	}
}
