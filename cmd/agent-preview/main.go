package main

import (
	"fmt"
	"os"

	"github.com/Zhangs-11/agent-preview/internal/app"
)

func main() {
	if err := app.Run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "agent-preview:", err)
		os.Exit(1)
	}
}
