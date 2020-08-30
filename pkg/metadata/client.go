/*
Copyright 2016 Mirantis

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

package metadata

import (
	"github.com/boltdb/bolt"
	"github.com/golang/glog"
)

type boltClient struct {
	db *bolt.DB
}

// NewStore is a factory function for Store interface
func NewStore(path string) (Store, error) {
	db, err := bolt.Open(path, 0600, nil)
	if err != nil {
		return nil, err
	}

	client := &boltClient{db: db}
	return client, nil
}

// Close releases all database resources
func (b boltClient) Close() error {
	return b.db.Close()
}

// TODO: Verify libvirt domain update info or callbacks status before reset the resource update in progress flag
//       if libvirt is still updating it, the don't reset it
//
func (b boltClient) ResetResourceUpdateInProgress() error {
	glog.V(4).Infof("Reset container resource update in progress")
	sandboxes, err := b.ListPodSandboxes(nil)
	if err != nil {
		return err
	}

	for _, sandbox := range sandboxes {
		containers, err := b.ListPodContainers(sandbox.GetID())
		if err != nil {
			return err
		}

		for _, container := range containers {
			containerInfo, err := container.Retrieve()
			if err != nil {
				return err
			}

			if containerInfo.Config.ResourceUpdateInProgress == true {
				glog.Infof("Reset container resource update in progress flag for container %v", container.GetID())
				b.SetResourceUpdateInProgress(container.GetID(), false)
			}
		}
	}

	return nil
}
