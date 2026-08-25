// Command jt is a fast, vim-native TUI for Jira Cloud.
//
// It speaks REST directly rather than shelling out per action: a persistent
// connection amortizes the TLS handshake across the session, and HTTP status
// codes, error bodies and Retry-After stay visible. The Atlassian CLI remains
// available as an explicit escape hatch from the commandline.
package main

import (
	"fmt"
	"os"
)

// version is set at build time via -ldflags.
var version = "dev"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "jt:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) > 0 && (args[0] == "version" || args[0] == "--version") {
		fmt.Println("jt", version)
		return nil
	}
	panic("not implemented")
}
