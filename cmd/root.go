package cmd

import (
	"github.com/counteractive/o365beat/beater"

	cmd "github.com/elastic/beats/libbeat/cmd"
	"github.com/elastic/beats/libbeat/cmd/instance"
)

// Name of this beat
var Name = "o365beat"

// RootCmd to handle beats cli
var RootCmd = cmd.GenRootCmdWithSettings(beater.New, instance.Settings{Name: Name})
