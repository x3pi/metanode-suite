package common

import "fmt"

const (
	ColorReset  = "\033[0m"
	ColorRed    = "\033[31m"
	ColorGreen  = "\033[32m"
	ColorYellow = "\033[33m"
	ColorCyan   = "\033[36m"
	ColorBold   = "\033[1m"
	ColorPurple = "\033[35m"
)

func PrintHeader(title string) {
	fmt.Println()
	fmt.Printf("%s%s=== %s ===%s\n", ColorCyan, ColorBold, title, ColorReset)
}

func PrintSuccess(msg string) {
	fmt.Printf("%s✅ %s%s\n", ColorGreen, msg, ColorReset)
}

func PrintWarn(msg string) {
	fmt.Printf("%s⚠️  %s%s\n", ColorYellow, msg, ColorReset)
}

func PrintError(msg string) {
	fmt.Printf("%s❌ %s%s\n", ColorRed, msg, ColorReset)
}
