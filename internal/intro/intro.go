// Package intro displays the CLI banner.
package intro

import (
	"fmt"

	"github.com/pterm/pterm"

	"github.com/NodeOps-app/createos-cli/internal/pkg/version"
	"github.com/NodeOps-app/createos-cli/internal/updater"
)

const asciiLogo = ` ██████╗██████╗ ███████╗ █████╗ ████████╗███████╗ ██████╗ ███████╗
██╔════╝██╔══██╗██╔════╝██╔══██╗╚══██╔══╝██╔════╝██╔═══██╗██╔════╝
██║     ██████╔╝█████╗  ███████║   ██║   █████╗  ██║   ██║███████╗
██║     ██╔══██╗██╔══╝  ██╔══██║   ██║   ██╔══╝  ██║   ██║╚════██║
╚██████╗██║  ██║███████╗██║  ██║   ██║   ███████╗╚██████╔╝███████║
 ╚═════╝╚═╝  ╚═╝╚══════╝╚═╝  ╚═╝  ╚═╝   ╚══════╝ ╚═════╝ ╚══════╝`

// Show prints the ASCII logo with cyan color and a subtitle
func Show() {
	style := pterm.NewStyle(pterm.FgCyan)
	style.Println(asciiLogo)

	style2 := pterm.NewStyle(pterm.FgGray)
	style2.Println("  Your intelligent infrastructure CLI (version: " + version.Version + ")")
	fmt.Println()

	if latest := updater.LatestVersion(); latest != "" {
		pterm.Info.Printf("A new version is available: %s → %s\n", version.Version, latest)
		pterm.Println(pterm.Gray("  Run 'createos upgrade' to update."))
		fmt.Println()
	}

	fmt.Println()
}
