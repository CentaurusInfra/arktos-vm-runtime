/*
Copyright 2020 Authors of Arktos

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

package libvirttools

import (
	"github.com/Mirantis/virtlet/pkg/metadata"
	"github.com/Mirantis/virtlet/pkg/metadata/types"
	"github.com/golang/glog"
	"github.com/libvirt/libvirt-go"
	"time"
)

// handle libvirt domain events
// currently it is merely for the memory device add/remove event to avoid repeated calls of
// UpdateContainerResources() from kubelet while a resource updating is in progress
//
type eventHandler struct {
	uri       string
	conn      *libvirt.Connect
	metaStore metadata.ContainerStore
}

func init() {
	libvirt.EventRegisterDefaultImpl()
}

func NewEventHandler(uri string, store metadata.Store) *eventHandler {
	conn, err := libvirt.NewConnect(uri)
	if err != nil {
		glog.Errorf("failed to connect to %v", uri)
		return nil
	}

	return &eventHandler{
		uri:       uri,
		conn:      conn,
		metaStore: store,
	}
}

// TODO: add failure handling logic in callback registration and handler logic
//       deregister if needed in failed cases
func (h *eventHandler) RegisterEventCallBacks() error {
	var callbackId int
	var err error

	if callbackId, err = h.conn.DomainEventLifecycleRegister(nil, func(c *libvirt.Connect, d *libvirt.Domain, event *libvirt.DomainEventLifecycle) {
		id, _ := d.GetUUIDString()
		glog.V(4).Infof("debug changes on Domain ID %v", id)

	}); err != nil {
		return err
	}
	glog.V(4).Infof("Registered domainLifecycleCallback returns %v, error: %v", callbackId, err)

	if callbackId, err = h.conn.DomainEventDeviceAddedRegister(nil, func(c *libvirt.Connect, d *libvirt.Domain, event *libvirt.DomainEventDeviceAdded) {
		glog.V(4).Infof("Device added. DevAlias :%v", event.DevAlias)
		handleMemoryDeviceAddRemove(d, h.metaStore)

	}); err != nil {
		return err
	}
	glog.V(4).Infof("Registered deviceAddedCallback returns %v, error: %v", callbackId, err)

	if callbackId, err = h.conn.DomainEventDeviceRemovedRegister(nil, func(c *libvirt.Connect, d *libvirt.Domain, event *libvirt.DomainEventDeviceRemoved) {
		glog.V(4).Infof("Device removed. DevAlias :%v; string: %v", event.DevAlias, event.String())
		handleMemoryDeviceAddRemove(d, h.metaStore)

	}); err != nil {
		return err
	}
	glog.V(4).Infof("Registered deviceRemovedCallback returns %v, error: %v", callbackId, err)

	// if async hotplug/unplug failed, release the lock so kubelet retry can get in
	if callbackId, err = h.conn.DomainEventDeviceRemovalFailedRegister(nil, func(c *libvirt.Connect, d *libvirt.Domain, event *libvirt.DomainEventDeviceRemovalFailed) {
		glog.V(4).Infof("Device removal failed. DevAlias :%v", event.DevAlias)
		handleMemoryDeviceAddRemove(d, h.metaStore)
	}); err != nil {
		return err
	}
	glog.V(4).Infof("Registered deviceRemovalFailedCallback returns %v, error: %v", callbackId, err)

	go func() {
		for {
			if res := libvirt.EventRunDefaultImpl(); res != nil {
				glog.Errorf("Listening to libvirt events failed, retrying.")
				time.Sleep(time.Second)
			}
		}
	}()
	glog.Infof("Listening to libvirt events......")
	return nil
}

// take actions needed in the callback functions
// keep synchronized pattern to reduce complexity for now
// post 830, a channel can be added here to perform those actions
func handleMemoryDeviceAddRemove(d *libvirt.Domain, metaStore metadata.ContainerStore) error {
	id, err := d.GetUUIDString()
	if err != nil {
		return err
	}

	domInfo, err := d.GetInfo()
	if err != nil {
		return err
	}

	// Update the vm config and metadata stored in Arktos-vm-runtime metadata
	containerInfo, err := metaStore.Container(id).Retrieve()
	if err != nil {
		return err
	}

	containerInfo.Config.MemoryLimitInBytes = int64(domInfo.Memory * defaultLibvirtDomainMemoryUnitValue)
	containerInfo.Config.ResourceUpdateInProgress = false

	glog.V(4).Infof("Update runtime metadata with config: %v", containerInfo.Config)
	err = metaStore.Container(id).Save(
		func(_ *types.ContainerInfo) (*types.ContainerInfo, error) {
			return containerInfo, nil
		})

	if err != nil {
		glog.Errorf("Failed to save containerInfo for container: %v", id)
		return err
	}

	return nil

}
