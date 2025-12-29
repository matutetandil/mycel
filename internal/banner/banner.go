// Package banner provides ASCII art banner and colored output for Mycel.
package banner

import (
	"fmt"
	"os"
	"runtime"
)

// ANSI color codes
const (
	Reset     = "\033[0m"
	Bold      = "\033[1m"
	Dim       = "\033[2m"

	// Colors
	Cyan      = "\033[36m"
	Green     = "\033[32m"
	Yellow    = "\033[33m"
	Blue      = "\033[34m"
	Magenta   = "\033[35m"
	White     = "\033[37m"

	// Bright colors
	BrightCyan    = "\033[96m"
	BrightGreen   = "\033[92m"
	BrightYellow  = "\033[93m"
	BrightBlue    = "\033[94m"
	BrightMagenta = "\033[95m"
	BrightWhite   = "\033[97m"
)

// colorsEnabled determines if colors should be used.
var colorsEnabled = true

func init() {
	// Disable colors on Windows (unless using Windows Terminal)
	if runtime.GOOS == "windows" {
		// Check for Windows Terminal or other modern terminals
		if os.Getenv("WT_SESSION") == "" && os.Getenv("TERM_PROGRAM") == "" {
			colorsEnabled = false
		}
	}

	// Respect NO_COLOR environment variable
	if os.Getenv("NO_COLOR") != "" {
		colorsEnabled = false
	}
}

// color applies color if colors are enabled.
func color(c, text string) string {
	if !colorsEnabled {
		return text
	}
	return c + text + Reset
}

// Print prints the Mycel banner with version info.
func Print(version string) {
	banner := `
    ` + color(BrightCyan, `███╗   ███╗██╗   ██╗ ██████╗███████╗██╗     `) + `
    ` + color(BrightCyan, `████╗ ████║╚██╗ ██╔╝██╔════╝██╔════╝██║     `) + `
    ` + color(Cyan, `██╔████╔██║ ╚████╔╝ ██║     █████╗  ██║     `) + `
    ` + color(Cyan, `██║╚██╔╝██║  ╚██╔╝  ██║     ██╔══╝  ██║     `) + `
    ` + color(Blue, `██║ ╚═╝ ██║   ██║   ╚██████╗███████╗███████╗`) + `
    ` + color(Blue, `╚═╝     ╚═╝   ╚═╝    ╚═════╝╚══════╝╚══════╝`) + `
    ` + color(Dim, "Declarative Microservice Framework") + ` ` + color(Dim, "v"+version) + `
`
	fmt.Print(banner)
	fmt.Println()
}

// PrintServiceInfo prints service configuration info.
func PrintServiceInfo(serviceName, serviceVersion, environment string, port int) {
	fmt.Printf("    %s %s %s\n",
		color(Dim, "Service:"),
		color(BrightWhite, serviceName),
		color(BrightGreen, "v"+serviceVersion),
	)
	fmt.Printf("    %s %s\n", color(Dim, "Environment:"), color(Yellow, environment))
	if port > 0 {
		fmt.Printf("    %s %s\n", color(Dim, "Port:"), color(BrightCyan, fmt.Sprintf("%d", port)))
	}
	fmt.Println()
}

// PrintConnector prints connector initialization info.
func PrintConnector(name, connType string, details string) {
	checkmark := color(BrightGreen, "✓")
	fmt.Printf("    %s %s %s %s\n",
		checkmark,
		color(White, name),
		color(Dim, "("+connType+")"),
		color(Dim, details),
	)
}

// PrintFlow prints flow registration info.
func PrintFlow(method, path, target string) {
	methodColor := methodToColor(method)
	fmt.Printf("      %s %s → %s\n",
		color(methodColor, padMethod(method)),
		color(White, path),
		color(Dim, target),
	)
}

// PrintReady prints the ready message.
func PrintReady() {
	fmt.Println()
	fmt.Printf("    %s %s\n\n",
		color(BrightGreen, "✓"),
		color(BrightWhite, "Ready! Press Ctrl+C to stop."),
	)
}

// PrintShutdown prints shutdown message.
func PrintShutdown() {
	fmt.Println()
	fmt.Printf("    %s\n", color(Yellow, "Shutting down gracefully..."))
}

// PrintGoodbye prints goodbye message.
func PrintGoodbye() {
	fmt.Printf("    %s %s\n\n", color(BrightGreen, "✓"), color(Dim, "Goodbye!"))
}

// PrintError prints an error message.
func PrintError(msg string) {
	fmt.Printf("    %s %s\n", color(BrightMagenta, "✗"), color(BrightMagenta, msg))
}

// methodToColor returns the appropriate color for an HTTP/TCP method.
func methodToColor(method string) string {
	switch method {
	case "GET":
		return BrightGreen
	case "POST":
		return BrightYellow
	case "PUT", "PATCH":
		return BrightBlue
	case "DELETE":
		return BrightMagenta
	case "TCP":
		return BrightCyan
	default:
		return White
	}
}

// padMethod pads HTTP method to fixed width for alignment.
func padMethod(method string) string {
	return fmt.Sprintf("%-6s", method)
}
