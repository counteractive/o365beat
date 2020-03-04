package cmd

import (
	"github.com/counteractive/o365beat/beater"

	cmd "github.com/elastic/beats/libbeat/cmd"
	"github.com/elastic/beats/libbeat/cmd/instance"
)

var name = "o365beat"
var version = "1.5.1" // TODO consider moving this or pulling from conf or env

// RootCmd to handle beats cli
var RootCmd = cmd.GenRootCmdWithSettings(beater.New, instance.Settings{Name: name, Version: version})
