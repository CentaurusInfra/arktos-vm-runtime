/*
Copyright 2017 Mirantis
Copyright 2020 Authors of Arktos – file modified

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
	"fmt"
	"github.com/golang/glog"
	"github.com/libvirt/libvirt-go"
	"github.com/libvirt/libvirt-go-xml"
	"math"

	"github.com/Mirantis/virtlet/pkg/virt"
)

type libvirtDomainConnection struct {
	conn libvirtConnection
}

// TODO: runtime issue: https://github.com/futurewei-cloud/arktos-vm-runtime/issues/50
//       multiple sizes of devices, and, numa node setting
// default mem chip size set to 512 MiB
const memoryDeviceSizeInKiB = 512 * 1024

const memoryDeviceDefinition = `<memory model='dimm'>
							<target>
								<size unit='MiB'>512</size>
								<node>0</node>
							</target>
						</memory>`

const snapshotXMLTemplate = `<domainsnapshot>
  								<name>%s</name>
							 </domainsnapshot>`

var _ virt.DomainConnection = &libvirtDomainConnection{}

func newLibvirtDomainConnection(conn libvirtConnection) *libvirtDomainConnection {
	return &libvirtDomainConnection{conn: conn}
}

func (dc *libvirtDomainConnection) DefineDomain(def *libvirtxml.Domain) (virt.Domain, error) {
	xml, err := def.Marshal()
	if err != nil {
		return nil, err
	}
	glog.V(2).Infof("Defining domain:\n%s", xml)
	d, err := dc.conn.invoke(func(c *libvirt.Connect) (interface{}, error) {
		return c.DomainDefineXML(xml)
	})
	if err != nil {
		return nil, err
	}
	return &libvirtDomain{d.(*libvirt.Domain)}, nil
}

func (dc *libvirtDomainConnection) ListDomains() ([]virt.Domain, error) {
	domains, err := dc.conn.invoke(func(c *libvirt.Connect) (interface{}, error) {
		return c.ListAllDomains(0)
	})
	if err != nil {
		return nil, err
	}
	var r []virt.Domain
	for _, d := range domains.([]libvirt.Domain) {
		// need to make a copy here
		curDomain := d
		r = append(r, &libvirtDomain{&curDomain})
	}
	return r, nil
}

func (dc *libvirtDomainConnection) LookupDomainByName(name string) (virt.Domain, error) {
	d, err := dc.conn.invoke(func(c *libvirt.Connect) (interface{}, error) {
		return c.LookupDomainByName(name)
	})
	if err != nil {
		libvirtErr, ok := err.(libvirt.Error)
		if ok && libvirtErr.Code == libvirt.ERR_NO_DOMAIN {
			return nil, virt.ErrDomainNotFound
		}
		return nil, err
	}
	return &libvirtDomain{d.(*libvirt.Domain)}, nil
}

func (dc *libvirtDomainConnection) LookupDomainByUUIDString(uuid string) (virt.Domain, error) {
	d, err := dc.conn.invoke(func(c *libvirt.Connect) (interface{}, error) {
		return c.LookupDomainByUUIDString(uuid)
	})
	if err != nil {
		libvirtErr, ok := err.(libvirt.Error)
		if ok && libvirtErr.Code == libvirt.ERR_NO_DOMAIN {
			return nil, virt.ErrDomainNotFound
		}
		return nil, err
	}
	return &libvirtDomain{d.(*libvirt.Domain)}, nil
}

func (dc *libvirtDomainConnection) DefineSecret(def *libvirtxml.Secret) (virt.Secret, error) {
	xml, err := def.Marshal()
	if err != nil {
		return nil, err
	}
	secret, err := dc.conn.invoke(func(c *libvirt.Connect) (interface{}, error) {
		return c.SecretDefineXML(xml, 0)
	})
	if err != nil {
		return nil, err
	}
	return &libvirtSecret{secret.(*libvirt.Secret)}, nil
}

func (dc *libvirtDomainConnection) LookupSecretByUUIDString(uuid string) (virt.Secret, error) {
	secret, err := dc.conn.invoke(func(c *libvirt.Connect) (interface{}, error) {
		return c.LookupSecretByUUIDString(uuid)
	})
	if err != nil {
		libvirtErr, ok := err.(libvirt.Error)
		if ok && libvirtErr.Code == libvirt.ERR_NO_SECRET {
			return nil, virt.ErrSecretNotFound
		}
		return nil, err
	}
	return &libvirtSecret{secret.(*libvirt.Secret)}, nil
}

func (dc *libvirtDomainConnection) LookupSecretByUsageName(usageType string, usageName string) (virt.Secret, error) {

	if usageType != "ceph" {
		return nil, fmt.Errorf("unsupported type %q for secret with usage name: %q", usageType, usageName)
	}

	secret, err := dc.conn.invoke(func(c *libvirt.Connect) (interface{}, error) {
		return c.LookupSecretByUsage(libvirt.SECRET_USAGE_TYPE_CEPH, usageName)
	})
	if err != nil {
		libvirtErr, ok := err.(libvirt.Error)
		if ok && libvirtErr.Code == libvirt.ERR_NO_SECRET {
			return nil, virt.ErrSecretNotFound
		}
		return nil, err
	}
	return &libvirtSecret{secret.(*libvirt.Secret)}, nil
}

type libvirtDomain struct {
	d *libvirt.Domain
}

var _ virt.Domain = &libvirtDomain{}

func (domain *libvirtDomain) Create() error {
	return domain.d.Create()
}

func (domain *libvirtDomain) Destroy() error {
	return domain.d.Destroy()
}

func (domain *libvirtDomain) Undefine() error {
	return domain.d.Undefine()
}

func (domain *libvirtDomain) Shutdown() error {
	return domain.d.Shutdown()
}

func (domain *libvirtDomain) State() (virt.DomainState, error) {
	di, err := domain.d.GetInfo()
	if err != nil {
		return virt.DomainStateNoState, err
	}
	switch di.State {
	case libvirt.DOMAIN_NOSTATE:
		return virt.DomainStateNoState, nil
	case libvirt.DOMAIN_RUNNING:
		return virt.DomainStateRunning, nil
	case libvirt.DOMAIN_BLOCKED:
		return virt.DomainStateBlocked, nil
	case libvirt.DOMAIN_PAUSED:
		return virt.DomainStatePaused, nil
	case libvirt.DOMAIN_SHUTDOWN:
		return virt.DomainStateShutdown, nil
	case libvirt.DOMAIN_CRASHED:
		return virt.DomainStateCrashed, nil
	case libvirt.DOMAIN_PMSUSPENDED:
		return virt.DomainStatePMSuspended, nil
	case libvirt.DOMAIN_SHUTOFF:
		return virt.DomainStateShutoff, nil
	default:
		return virt.DomainStateNoState, fmt.Errorf("bad domain state %v", di.State)
	}
}

func (domain *libvirtDomain) UUIDString() (string, error) {
	return domain.d.GetUUIDString()
}

func (domain *libvirtDomain) Name() (string, error) {
	return domain.d.GetName()
}

func (domain *libvirtDomain) XML() (*libvirtxml.Domain, error) {
	desc, err := domain.d.GetXMLDesc(libvirt.DOMAIN_XML_INACTIVE)
	if err != nil {
		return nil, err
	}
	var d libvirtxml.Domain
	if err := d.Unmarshal(desc); err != nil {
		return nil, fmt.Errorf("error unmarshalling domain definition: %v", err)
	}
	return &d, nil
}

// GetRSS returns RSS used by VM in bytes
func (domain *libvirtDomain) GetRSS() (uint64, error) {
	stats, err := domain.d.MemoryStats(uint32(libvirt.DOMAIN_MEMORY_STAT_LAST), 0)
	if err != nil {
		return 0, err
	}
	for _, stat := range stats {
		if stat.Tag == int32(libvirt.DOMAIN_MEMORY_STAT_RSS) {
			return stat.Val * 1024, nil
		}
	}
	return 0, fmt.Errorf("rss not found in memory stats")
}

// GetCPUTime returns cpu time used by VM in nanoseconds per core
func (domain *libvirtDomain) GetCPUTime() (uint64, error) {
	// all vcpus as a single value
	stats, err := domain.d.GetCPUStats(-1, 1, 0)
	if err != nil {
		return 0, err
	}
	if len(stats) != 1 {
		return 0, fmt.Errorf("domain.GetCPUStats returned %d values while single one was expected", len(stats))
	}
	if !stats[0].CpuTimeSet {
		return 0, fmt.Errorf("domain.CpuTime not found in memory stats")
	}
	return stats[0].CpuTime, nil
}

// Reboot reboots current domain
func (domain *libvirtDomain) Reboot(flags libvirt.DomainRebootFlagValues) error {
	return domain.d.Reboot(flags)
}

// CreateSnapshop creates a system snapshot for current domain
func (domain *libvirtDomain) CreateSnapshot(snapshotID string) error {
	spec := fmt.Sprintf(snapshotXMLTemplate, snapshotID)

	// with flag 0 it will create a system snapshot for an active domain.
	_, err := domain.d.CreateSnapshotXML(spec, 0)
	return err
}

func (domain *libvirtDomain) RestoreToSnapshot(snapshotID string) error {
	// the flag is not used in libvirt. Now it is requird to be always o.
	snapshot, err := domain.d.SnapshotLookupByName(snapshotID, 0)
	if err != nil {
		return fmt.Errorf("Failed to retrieve snapshot %s", snapshotID)
	}

	// the default flag 0 means reverting to the domain state when the snapshot
	// is taken, whether active or inactive
	return snapshot.RevertToSnapshot(0)
}

// Update domain vcpu
func (domain *libvirtDomain) SetVcpus(vcpus uint) error {
	return domain.d.SetVcpusFlags(vcpus, libvirt.DOMAIN_VCPU_CONFIG|libvirt.DOMAIN_VCPU_LIVE)
}

// TODO: move this to a helper function file
func determineNumberOfDeviceNeeded(memChangeInKib int64, isAttach bool) int {
	var numberMemoryDevicesNeeded int

	temp := math.Abs(float64(memChangeInKib)) / float64(memoryDeviceSizeInKiB)
	if isAttach {
		numberMemoryDevicesNeeded = int(math.Ceil(temp))
	} else {
		numberMemoryDevicesNeeded = int(math.Floor(temp))
	}

	return numberMemoryDevicesNeeded
}

// Update domain current memory
// the memory device is 512 Mib each
func (domain *libvirtDomain) AdjustDomainMemory(memChangeInKib int64) error {
	glog.V(4).Infof("MemoryChanges in KiB: %v", memChangeInKib)

	isAttach := memChangeInKib > 0
	glog.V(4).Infof("isAttach: %v", isAttach)

	numberMemoryDevicesNeeded := determineNumberOfDeviceNeeded(memChangeInKib, isAttach)
	glog.V(4).Infof("Number of device needed : %v", numberMemoryDevicesNeeded)

	// TODO: pending design
	// if number of device needed is 0, and the memory delta is not 0
	// it means the requested resource is less than the smallest supported device size
	// to attach or detach, consider this is an error case for now.
	// if the hutplug/unplug approach is eventually the ONLY way to support vertical scaling, the minimal device size
	// will need to be aware to VPA so it will round the request to match it. or the round-up is done at the runtime.
	// TODO: create an error package in project and move all hardcoded error string to it
	if numberMemoryDevicesNeeded == 0 {
		return fmt.Errorf("invalid memory change size")
	}

	for i := 0; i < numberMemoryDevicesNeeded; i++ {
		var err error
		if isAttach {
			glog.V(4).Infof("Attach memory device to domain")
			err = domain.d.AttachDeviceFlags(memoryDeviceDefinition, libvirt.DOMAIN_DEVICE_MODIFY_CONFIG|libvirt.DOMAIN_DEVICE_MODIFY_LIVE)
		} else {
			glog.V(4).Infof("Detach memory device to domain")
			err = domain.d.DetachDeviceFlags(memoryDeviceDefinition, libvirt.DOMAIN_DEVICE_MODIFY_CONFIG|libvirt.DOMAIN_DEVICE_MODIFY_LIVE)
		}
		if err != nil {
			return err
		}
	}

	return nil
}

type libvirtSecret struct {
	s *libvirt.Secret
}

func (secret *libvirtSecret) SetValue(value []byte) error {
	return secret.s.SetValue(value, 0)
}

func (secret *libvirtSecret) Remove() error {
	return secret.s.Undefine()
}
