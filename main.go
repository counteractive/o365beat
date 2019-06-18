package main

import (
	"os"

	"github.com/counteractive/o365beat/cmd"

	_ "github.com/counteractive/o365beat/include"
)

func main() {
	if err := cmd.RootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
