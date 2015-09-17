//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/huawei-openlab/oci2aci/Godeps/_workspace/src/github.com/appc/spec/schema"
	"github.com/huawei-openlab/oci2aci/Godeps/_workspace/src/github.com/appc/spec/schema/types"
	"github.com/huawei-openlab/oci2aci/Godeps/_workspace/src/github.com/opencontainers/specs"
)

// Entry point of oci2aci,
// First convert oci layout to aci layout, then build aci layout to image.
func runOCI2ACI(path string, flagDebug bool) error {
	if flagDebug {
		InitDebug()
	}
	if bValidate := validateOCIProc(path); bValidate != true {
		log.Printf("Conversion stop.")
		return nil
	}

	dirWork := createWorkDir()
	// First, convert layout
	err := convertLayout(path, dirWork)
	if err != nil {
		if debugEnabled {
			log.Printf("Conversion from oci to aci layout failed: %v", err)
		}

	}
	// Second, build image
	err = buildACI(dirWork)
	if err != nil {
		if debugEnabled {
			log.Printf("Generate aci image failed!")
		}
	} else {
		if debugEnabled {
			log.Printf("aci image generated successfully.")
		}
	}

	return nil
}

// Create work directory for the conversion output
func createWorkDir() string {
	idir, err := ioutil.TempDir("", "oci2aci")
	if err != nil {
		return ""
	}
	rootfs := filepath.Join(idir, "rootfs")
	os.MkdirAll(rootfs, 0755)

	data := []byte{}
	if err := ioutil.WriteFile(filepath.Join(idir, "manifest"), data, 0644); err != nil {
		return ""
	}
	return idir
}

// The structure of appc manifest:
// 1.acKind
// 2. acVersion
// 3. name
// 4. labels
//	4.1 version
//	4.2 os
//	4.3 arch
// 5. app
//	5.1 exec
//	5.2 user
//	5.3 group
//	5.4 eventHandlers
//	5.5 workingDirectory
//	5.6 environment
//	5.7 mountPoints
//	5.8 ports
//      5.9 isolators
// 6. annotations
//	6.1 created
//	6.2 authors
//	6.3 homepage
//	6.4 documentation
// 7. dependencies
//	7.1 imageName
//	7.2 imageID
//	7.3 labels
//	7.4 size
// 8. pathWhitelist

func genManifest(path string) *schema.ImageManifest {
	// Get runtime.json and config.json
	runtimePath := path + "/runtime.json"
	configPath := path + "/config.json"

	runtime, err := ioutil.ReadFile(runtimePath)
	if err != nil {
		return nil
	}

	config, err := ioutil.ReadFile(configPath)
	if err != nil {
		return nil
	}

	var spec specs.Spec
	err = json.Unmarshal(config, &spec)
	if err != nil {
		return nil
	}

	var runSpec specs.RuntimeSpec
	err = json.Unmarshal(runtime, &runSpec)
	if err != nil {
		return nil
	}

	// Begin to convert runtime.json/config.json to manifest
	m := new(schema.ImageManifest)

	// 1. Assemble "acKind" field
	m.ACKind = "ImageManifest"

	// 2. Assemble "acVersion" field
	m.ACVersion = schema.AppContainerVersion

	// 3. Assemble "name" field
	m.Name = "example"

	// 4. Assemble "labels" field
	// 4.1 "version"
	label := new(types.Label)
	label.Name = types.ACIdentifier("version")
	label.Value = spec.Version
	m.Labels = append(m.Labels, *label)
	// 4.2 "os"
	label = new(types.Label)
	label.Name = types.ACIdentifier("os")
	label.Value = spec.Platform.OS
	m.Labels = append(m.Labels, *label)
	// 4.3 "arch"
	label = new(types.Label)
	label.Name = types.ACIdentifier("arch")
	label.Value = spec.Platform.Arch
	m.Labels = append(m.Labels, *label)

	// 5. Assemble "app" field
	app := new(types.App)
	// 5.1 "exec"
	app.Exec = spec.Process.Args
	// 5.2 "user"
	app.User = fmt.Sprintf("%d", spec.Process.User.UID)
	// 5.3 "group"
	app.Group = fmt.Sprintf("%d", spec.Process.User.GID)
	// 5.4 "eventHandlers"
	event := new(types.EventHandler)
	event.Name = "pre-start"
	for index := range runSpec.Hooks.Prestart {
		event.Exec = append(event.Exec, runSpec.Hooks.Prestart[index].Path)
		event.Exec = append(event.Exec, runSpec.Hooks.Prestart[index].Args...)
		event.Exec = append(event.Exec, runSpec.Hooks.Prestart[index].Env...)
	}
	app.EventHandlers = append(app.EventHandlers, *event)
	event = new(types.EventHandler)
	event.Name = "post-stop"
	for index := range runSpec.Hooks.Poststop {
		event.Exec = append(event.Exec, runSpec.Hooks.Poststop[index].Path)
		event.Exec = append(event.Exec, runSpec.Hooks.Poststop[index].Args...)
		event.Exec = append(event.Exec, runSpec.Hooks.Poststop[index].Env...)
	}
	app.EventHandlers = append(app.EventHandlers, *event)
	// 5.5 "workingDirectory"
	app.WorkingDirectory = spec.Process.Cwd
	// 5.6 "environment"
	env := new(types.EnvironmentVariable)
	for index := range spec.Process.Env {
		s := strings.Split(spec.Process.Env[index], "=")
		env.Name = s[0]
		env.Value = s[1]
		app.Environment = append(app.Environment, *env)
	}

	// 5.7 "mountPoints"
	for index := range spec.Mounts {
		mount := new(types.MountPoint)
		mount.Name = types.ACName(spec.Mounts[index].Name)
		mount.Path = spec.Mounts[index].Path
		app.MountPoints = append(app.MountPoints, *mount)
	}

	// 5.8 "ports"

	// 5.9 "isolators"

	// 6. "annotations"

	// 7. "dependencies"

	// 8. "pathWhitelist"

	m.App = app

	return m
}

// Convert OCI layout to ACI layout
func convertLayout(srcPath, dstPath string) error {
	src, _ := filepath.Abs(srcPath)
	src += "/rootfs"
	if err := run(exec.Command("cp", "-rf", src, dstPath)); err != nil {
		return err
	}

	m := genManifest(srcPath)

	bytes, err := json.Marshal(m)
	if err != nil {
		return err
	}

	manifestPath := dstPath + "/manifest"

	ioutil.WriteFile(manifestPath, bytes, 0644)
	return nil
}
