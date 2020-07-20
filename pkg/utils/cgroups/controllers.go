/*
Copyright 2018 Mirantis

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package cgroups

import (
	"fmt"
	"github.com/containerd/cgroups"
	"github.com/golang/glog"
	"github.com/opencontainers/runtime-spec/specs-go"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/Mirantis/virtlet/pkg/fs"
	"github.com/Mirantis/virtlet/pkg/utils"
)

const (
	cgroupfs = "/sys/fs/cgroup"
)

// Controller represents a named controller for a process
type Controller struct {
	fsys fs.FileSystem
	name string
	path string
}

// Manager provides an interface to operate on linux cgroups
type Manager interface {
	// GetProcessControllers returns the mapping between controller types and
	// their paths inside cgroup fs for the specified PID.
	GetProcessControllers() (map[string]string, error)
	// GetProcessController returns a named resource Controller for the specified PID.
	GetProcessController(controllerName string) (*Controller, error)
	// MoveProcess move the process to the path under a cgroup controller
	MoveProcess(controller, path string) error
}

// RealManager provides an implementation of Manager which is
// using default linux system paths to access info about cgroups for processes.
type RealManager struct {
	fsys fs.FileSystem
	pid  string
}

var _ Manager = &RealManager{}

// NewManager returns an instance of RealManager
func NewManager(pid interface{}, fsys fs.FileSystem) Manager {
	if fsys == nil {
		fsys = fs.RealFileSystem
	}
	return &RealManager{fsys: fsys, pid: utils.Stringify(pid)}
}

// GetProcessControllers is an implementation of GetProcessControllers method
// of Manager interface.
func (c *RealManager) GetProcessControllers() (map[string]string, error) {
	fr, err := c.fsys.GetDelimitedReader(filepath.Join("/proc", c.pid, "cgroup"))
	if err != nil {
		return nil, err
	}
	defer fr.Close()

	ctrls := make(map[string]string)

	for {
		line, err := fr.ReadString('\n')
		if err != nil {
			if err != io.EOF {
				return nil, err
			}
		}

		// strip eol
		line = strings.Trim(line, "\n")
		if line == "" {
			break
		}

		// split entries like:
		// "6:memory:/user.slice/user-xxx.slice/session-xx.scope"
		parts := strings.SplitN(line, ":", 3)

		name := parts[1]
		if strings.HasPrefix(name, "name=") {
			// Handle named cgroup hierarchies like name=systemd
			// The corresponding directory tree will be /sys/fs/cgroup/systemd
			name = name[5:]
		}

		// use second part as controller name and third as its path
		ctrls[name] = parts[2]

		if err == io.EOF {
			break
		}
	}

	return ctrls, nil
}

// GetProcessController is an implementation of GetProcessController method
// of Manager interface.
func (c *RealManager) GetProcessController(controllerName string) (*Controller, error) {
	controllers, err := c.GetProcessControllers()
	if err != nil {
		return nil, err
	}

	controllerPath, ok := controllers[controllerName]
	if !ok {
		return nil, fmt.Errorf("controller %q for process %v not found", controllerName, c.pid)
	}

	return &Controller{
		fsys: c.fsys,
		name: controllerName,
		path: controllerPath,
	}, nil
}

// MoveProcess implements MoveProcess method of Manager
func (c *RealManager) MoveProcess(controller, path string) error {
	return c.fsys.WriteFile(
		filepath.Join(cgroupfs, controller, path, "cgroup.procs"),
		[]byte(utils.Stringify(c.pid)),
		0644,
	)
}

// Set sets the value of a controller setting
func (c *Controller) Set(name string, value interface{}) error {
	return c.fsys.WriteFile(
		filepath.Join(cgroupfs, c.name, c.path, c.name+"."+name),
		[]byte(utils.Stringify(value)),
		0644,
	)
}

// Check if a particular cgroup exists for a given controller
func (c *Controller) CgroupExists(ctl string, cgPath string) bool {
	fullPath := path.Join(cgroupfs, ctl, cgPath)
	_, err := os.Stat(fullPath)

	if os.IsNotExist(err) {
		glog.V(5).Infof("Cgroup path %v does not exist", fullPath)
		return false
	}

	// log err for investigation and return true
	glog.V(5).Infof("Cgroup path check with error: %v", err)
	return true

}

// Create a new CGroup with desired resource settings
func CreateChildCgroup(cgParent string, cgName string, res *specs.LinuxResources) (cgroups.Cgroup, error) {
	// if cgParent is not set, default to root
	if cgParent == "" {
		cgParent = "/"
	}

	parent, err := cgroups.Load(cgroups.V1, cgroups.StaticPath(cgParent))
	if err != nil {
		glog.Errorf("Failed to load parent cgroup %v. error %v", cgParent, err)
		return nil, err
	}

	cg, err := parent.New(cgName, res)
	if err != nil {
		glog.Errorf("Failed to create the child cgroup %v. error %v", cgName, err)
		return nil, err
	}

	return cg, nil
}

// Update a CGroup with desired resource settings
func UpdateVmCgroup(cgPath string, res *specs.LinuxResources) error {
	glog.V(4).Infof("Update VM Cgroup: %v, with resource %v", cgPath, res)
	cg, err := cgroups.Load(cgroups.V1, cgroups.StaticPath(cgPath))
	if err != nil {
		glog.Errorf("Failed to load cgroup %v. error %v", cgPath, err)
		return err
	}

	err = cg.Update(res)
	if err != nil {
		glog.Errorf("Failed to update cgroup %v. error %v", cgPath, err)
		return err
	}

	return nil
}
