package intro

import (
	"fmt"
	"time"

	"github.com/pterm/pterm"
)

const asciiLogo = `
 ██████╗██████╗ ███████╗ █████╗ ████████╗███████╗ ██████╗ ███████╗
██╔════╝██╔══██╗██╔════╝██╔══██╗╚══██╔══╝██╔════╝██╔═══██╗██╔════╝
██║     ██████╔╝█████╗  ███████║   ██║   █████╗  ██║   ██║███████╗
██║     ██╔══██╗██╔══╝  ██╔══██║   ██║   ██╔══╝  ██║   ██║╚════██║
╚██████╗██║  ██║███████╗██║  ██║   ██║   ███████╗╚██████╔╝███████║
 ╚═════╝╚═╝  ╚═╝╚══════╝╚═╝  ╚═╝  ╚═╝   ╚══════╝ ╚═════╝ ╚══════╝`

// Show prints the ASCII logo with cyan color and a subtitle
func Show() {
	style := pterm.NewStyle(pterm.FgCyan)
	style.Println(asciiLogo)

	typewriterPrint("  Your intelligent infrastructure CLI", 30*time.Millisecond)
	fmt.Println()
	fmt.Println()
}

func typewriterPrint(text string, delay time.Duration) {
	style := pterm.NewStyle(pterm.FgGray)
	for _, ch := range text {
		style.Print(string(ch))
		time.Sleep(delay)
	}
}
