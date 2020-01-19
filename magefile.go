// Licensed to Elasticsearch B.V. under one or more contributor
// license agreements. See the NOTICE file distributed with
// this work for additional information regarding copyright
// ownership. Elasticsearch B.V. licenses this file to you under
// the Apache License, Version 2.0 (the "License"); you may
// not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing,
// software distributed under the License is distributed on an
// "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
// KIND, either express or implied.  See the License for the
// specific language governing permissions and limitations
// under the License.

// +build mage

package main

import (
	"context"
	"fmt"
	"io/ioutil"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/magefile/mage/mg"
	"github.com/magefile/mage/sh"

	devtools "github.com/elastic/beats/dev-tools/mage"
)

func init() {
	devtools.SetBuildVariableSources(devtools.DefaultBeatBuildVariableSources)
	devtools.BeatVendor = "Counteractive"
	devtools.BeatDescription = "Shipper for Office 365 logs from Management Activities API."
}

// Build builds the Beat binary.
func Build() error {
	return devtools.Build(devtools.DefaultBuildArgs())
}

// GolangCrossBuild build the Beat binary inside of the golang-builder.
// Do not use directly, use crossBuild instead.
func GolangCrossBuild() error {
	return devtools.GolangCrossBuild(devtools.DefaultGolangCrossBuildArgs())
}

// BuildGoDaemon builds the go-daemon binary (use crossBuildGoDaemon).
func BuildGoDaemon() error {
	return devtools.BuildGoDaemon()
}

// CrossBuild cross-builds the beat for all target platforms.
func CrossBuild() error {
	return devtools.CrossBuild()
}

// CrossBuildGoDaemon cross-builds the go-daemon binary using Docker.
func CrossBuildGoDaemon() error {
	return devtools.CrossBuildGoDaemon()
}

// Clean cleans all generated files and build artifacts.
func Clean() error {
	return devtools.Clean()
}

// Package packages the Beat for distribution.
// Use SNAPSHOT=true to build snapshots.
// Use PLATFORMS to control the target platforms.
func Package() {
	start := time.Now()
	defer func() { fmt.Println("package ran for", time.Since(start)) }()

	devtools.UseCommunityBeatPackaging()

	mg.Deps(Update)
	mg.Deps(CrossBuild, CrossBuildGoDaemon)
	mg.SerialDeps(devtools.Package, TestPackages)
}

// TestPackages tests the generated packages (i.e. file modes, owners, groups).
func TestPackages() error {
	return devtools.TestPackages()
}

// Update updates the generated files (aka make update).
func Update() error {
	return sh.Run("make", "update")
}

// Fields generates a fields.yml for the Beat.
func Fields() error {
	return devtools.GenerateFieldsYAML()
}

// GoTestUnit executes the Go unit tests.
// Use TEST_COVERAGE=true to enable code coverage profiling.
// Use RACE_DETECTOR=true to enable the race detector.
func GoTestUnit(ctx context.Context) error {
	return devtools.GoTest(ctx, devtools.DefaultGoTestUnitArgs())
}

// GoTestIntegration executes the Go integration tests.
// Use TEST_COVERAGE=true to enable code coverage profiling.
// Use RACE_DETECTOR=true to enable the race detector.
func GoTestIntegration(ctx context.Context) error {
	return devtools.GoTest(ctx, devtools.DefaultGoTestIntegrationArgs())
}

// Config generates both the short/reference/docker configs.
func Config() error {
	err := devtools.Config(devtools.AllConfigTypes, devtools.ConfigFileParams{}, ".")
	if err != nil {
		return err
	}

	// comment out additional processors section in short config (fixes #9)
	// targets known case of second section containing:
	//   processors:
	//     - add_host_metadata: ~
	//     - add_cloud_metadata: ~
	//     - add_docker_metadata: ~ # since libbeat 7.5.1

	configPath := filepath.Join(".", devtools.BeatName + ".yml")

	config, err := ioutil.ReadFile(configPath)
	if err != nil {
		return err
	}

	lines := strings.Split(string(config), "\n")

	processorsCount := 0
	for i, line := range lines {
		matches, _ := regexp.MatchString("^processors:", line)
		if matches {
			processorsCount += 1
			if processorsCount > 1 {
				// TODO: make more generic to comment out all remaining processors sections
				lines[i] = "# " + lines[i]
				lines[i + 1] = "# " + lines[i + 1]
				lines[i + 2] = "# " + lines[i + 2]
				lines[i + 3] = "# " + lines[i + 3]
				break
			}
		}
	}

	output := strings.Join(lines, "\n")
	err = ioutil.WriteFile(configPath, []byte(output), 0600)
	if err != nil {
		return err
	}

	// reference config file doesn't have additional _un-commented_ processors sections
	// TODO: equivalent fix for #9 in docker config (filepath.Join(targetDir, BeatName+".docker.yml"))

	return err
}
