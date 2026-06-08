package main

import (
	"fmt"
	"os"

	"github.com/dbuzatto/strix/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "strix:", err)
		os.Exit(1)
	}
}
