/*
Copyright 2016-2017 Mirantis
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
	"github.com/Mirantis/virtlet/pkg/utils/cgroups"
	"github.com/opencontainers/runtime-spec/specs-go"
	kubeapi "k8s.io/kubernetes/pkg/kubelet/apis/cri/runtime/v1alpha2"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/golang/glog"
	"github.com/jonboulle/clockwork"
	libvirtxml "github.com/libvirt/libvirt-go-xml"
	uuid "github.com/nu7hatch/gouuid"
	"k8s.io/apimachinery/pkg/fields"
	kubetypes "k8s.io/kubernetes/pkg/kubelet/types"

	vconfig "github.com/Mirantis/virtlet/pkg/config"
	"github.com/Mirantis/virtlet/pkg/fs"
	"github.com/Mirantis/virtlet/pkg/metadata"
	"github.com/Mirantis/virtlet/pkg/metadata/types"
	"github.com/Mirantis/virtlet/pkg/utils"
	"github.com/Mirantis/virtlet/pkg/virt"
	containerdCgroups "github.com/containerd/cgroups"
)

const (
	KiValue = 1024
	//MiValue = 1048576
	//GiValue = 1073741824
	// default unit in the domain is Kib
	defaultLibvirtDomainMemoryUnitValue = KiValue

	// default to 1Gi
	defaultMemory     = 1048576
	defaultMemoryUnit = "KiB"
	defaultDomainType = "kvm"
	defaultEmulator   = "/usr/bin/kvm"
	noKvmDomainType   = "qemu"
	noKvmEmulator     = "/usr/bin/qemu-system-x86_64"

	domainStartCheckInterval      = 250 * time.Millisecond
	domainStartTimeout            = 10 * time.Second
	domainShutdownRetryInterval   = 5 * time.Second
	domainShutdownOnRemoveTimeout = 60 * time.Second
	domainDestroyCheckInterval    = 500 * time.Millisecond
	domainDestroyTimeout          = 5 * time.Second

	// ContainerNsUUID template for container ns uuid generation
	ContainerNsUUID = "67b7fb47-7735-4b64-86d2-6d062d121966"

	// KubernetesPodNameLabel is pod name container label (copied from kubetypes).
	KubernetesPodNameLabel = "io.kubernetes.pod.name"
	// KubernetesPodNamespaceLabel is pod namespace container label (copied from kubetypes),
	KubernetesPodNamespaceLabel = "io.kubernetes.pod.namespace"
	// KubernetesPodUIDLabel is uid container label (copied from kubetypes).
	KubernetesPodUIDLabel = "io.kubernetes.pod.uid"
	// KubernetesContainerNameLabel is container name label (copied from kubetypes)
	KubernetesContainerNameLabel = "io.kubernetes.container.name"
)

type domainSettings struct {
	useKvm           bool
	domainName       string
	domainUUID       string
	memory           int
	memoryUnit       string
	vcpuNum          uint
	cpuShares        uint
	cpuPeriod        uint64
	cpuQuota         int64
	rootDiskFilepath string
	netFdKey         string
	enableSriov      bool
	cpuModel         string
	systemUUID       *uuid.UUID
}

func (ds *domainSettings) createDomain(config *types.VMConfig) *libvirtxml.Domain {
	domainType := defaultDomainType
	emulator := defaultEmulator
	if !ds.useKvm {
		domainType = noKvmDomainType
		emulator = noKvmEmulator
	}

	scsiControllerIndex := uint(0)
	domain := &libvirtxml.Domain{
		Devices: &libvirtxml.DomainDeviceList{
			Emulator: "/vmwrapper",
			Inputs: []libvirtxml.DomainInput{
				{Type: "tablet", Bus: "usb"},
			},
			Graphics: []libvirtxml.DomainGraphic{
				{VNC: &libvirtxml.DomainGraphicVNC{Port: -1}},
			},
			Videos: []libvirtxml.DomainVideo{
				{Model: libvirtxml.DomainVideoModel{Type: "cirrus"}},
			},
			Controllers: []libvirtxml.DomainController{
				{Type: "scsi", Index: &scsiControllerIndex, Model: "virtio-scsi"},
			},
		},

		OS: &libvirtxml.DomainOS{
			Type: &libvirtxml.DomainOSType{Type: "hvm"},
			BootDevices: []libvirtxml.DomainBootDevice{
				{Dev: "hd"},
			},
		},

		Features: &libvirtxml.DomainFeatureList{ACPI: &libvirtxml.DomainFeature{}},

		OnPoweroff: "destroy",
		OnReboot:   "restart",
		OnCrash:    "restart",

		Type: domainType,

		Name:          ds.domainName,
		UUID:          ds.domainUUID,
		Memory:        &libvirtxml.DomainMemory{Value: uint(ds.memory), Unit: defaultMemoryUnit},
		CurrentMemory: &libvirtxml.DomainCurrentMemory{Value: uint(ds.memory), Unit: defaultMemoryUnit},
		MaximumMemory: &libvirtxml.DomainMaxMemory{Value: getMaxMemoryInKiB(ds), Unit: defaultMemoryUnit, Slots: 16},
		VCPU: &libvirtxml.DomainVCPU{
			Current: ds.vcpuNum,
			Value:   getMaxVcpus(ds),
		},

		CPUTune: &libvirtxml.DomainCPUTune{
			Shares: &libvirtxml.DomainCPUTuneShares{Value: ds.cpuShares},
			Period: &libvirtxml.DomainCPUTunePeriod{Value: ds.cpuPeriod},
			Quota:  &libvirtxml.DomainCPUTuneQuota{Value: ds.cpuQuota},
		},
		// This causes '"qemu: qemu_thread_create: Resource temporarily unavailable"' QEMU errors
		// when Virtlet is run as a non-privileged user.
		// Under strace, it looks like a bunch of mmap()s failing with EAGAIN
		// which happens due to mlockall() call somewhere above that.
		// This could be worked around using setrlimit() but really
		// swap handling is not needed here because it's incorrect
		// to have swap enabled on the nodes of a real Kubernetes cluster.

		// MemoryBacking: &libvirtxml.DomainMemoryBacking{Locked: &libvirtxml.DomainMemoryBackingLocked{}},

		QEMUCommandline: &libvirtxml.DomainQEMUCommandline{
			Envs: []libvirtxml.DomainQEMUCommandlineEnv{
				{Name: vconfig.EmulatorEnvVarName, Value: emulator},
				{Name: vconfig.NetKeyEnvVarName, Value: ds.netFdKey},
				{Name: vconfig.ContainerIDEnvVarName, Value: config.DomainUUID},
				{Name: vconfig.LogPathEnvVarName,
					Value: filepath.Join(config.LogDirectory, config.LogPath)},
			},
		},
	}

	// TODO: expose this setting to users
	//       default cpuMode to hostModel in domainConfig
	//       this seems to be a wall we are hitting with the current annotation based setting
	//       design of the direct convert to domain definition can help here
	numaid := uint(0)
	// Set cpu model.
	// If user understand the cpu definition of libvirt,
	// the user is very professional, we prior to use it.
	if config.ParsedAnnotations.CPUSetting != nil {
		domain.CPU = config.ParsedAnnotations.CPUSetting
	} else {
		glog.V(4).Infof("Setting up CPU...")
		switch ds.cpuModel {
		case types.CPUModelHostModel:
			// The following enables nested virtualization.
			// In case of intel processors it requires nested=1 option
			// for kvm_intel module. That can be passed like this:
			// modprobe kvm_intel nested=1
			domain.CPU = &libvirtxml.DomainCPU{
				Mode: types.CPUModelHostModel,
				Model: &libvirtxml.DomainCPUModel{
					Fallback: "forbid",
				},
				Numa: &libvirtxml.DomainNuma{
					Cell: []libvirtxml.DomainCell{
						{
							ID:     &numaid,
							CPUs:   getMaxVcpusOrderString(getMaxVcpus(ds)),
							Memory: uint(ds.memory),
							Unit:   defaultMemoryUnit,
						},
					},
				},
			}
		case "":
			// leave it empty
		default:
			glog.Warningf("Unknown value set in VIRTLET_CPU_MODEL: %q", ds.cpuModel)
		}
	}

	if ds.systemUUID != nil {
		domain.SysInfo = []libvirtxml.DomainSysInfo{
			{
				FWCfg: &libvirtxml.DomainSysInfoFWCfg{
					Entry: []libvirtxml.DomainSysInfoEntry{
						{
							Name:  "uuid",
							Value: ds.systemUUID.String(),
						},
					},
				},
			},
		}
	}

	if ds.enableSriov {
		domain.QEMUCommandline.Envs = append(domain.QEMUCommandline.Envs,
			libvirtxml.DomainQEMUCommandlineEnv{Name: "VMWRAPPER_KEEP_PRIVS", Value: "1"})
	}

	if config.CgroupParent != "" {
		domain.QEMUCommandline.Envs = append(domain.QEMUCommandline.Envs,
			libvirtxml.DomainQEMUCommandlineEnv{Name: vconfig.VmCgroupParentEnvVarName, Value: path.Join(config.CgroupParent, domain.UUID)})
	}

	return domain
}

// Helper functions
// TODO: set max memory and CPU to host allocatable, it is controlled by the CG anyways
//       arktos runtime issue https://github.com/futurewei-cloud/arktos-vm-runtime/issues/44
// The ds has the memory set already
func getMaxMemoryInKiB(ds *domainSettings) uint {
	return uint(ds.memory * 2)
}

func getMaxVcpus(ds *domainSettings) uint {
	return 2 * ds.vcpuNum
}

func getMaxVcpusOrderString(vcpus uint) string {
	return fmt.Sprintf("0-%d", vcpus-1)
}

// VirtualizationConfig specifies configuration options for VirtualizationTool.
type VirtualizationConfig struct {
	// True if KVM should be disabled
	DisableKVM bool
	// True if SR-IOV support needs to be enabled
	EnableSriov bool
	// List of raw devices that can be accessed by the VM.
	RawDevices []string
	// Kubelet's root dir
	// FIXME: kubelet's --root-dir may be something other than /var/lib/kubelet
	// Need to remove it from daemonset mounts (both dev and non-dev)
	// Use 'nsenter -t 1 -m -- tar ...' or something to grab the path
	// from root namespace
	KubeletRootDir string
	// The path of streamer socket used for
	// logging. By default, the path is empty. When the path is empty,
	// logging is disabled for the VMs.
	StreamerSocketPath string
	// The name of libvirt volume pool to use for the VMs.
	VolumePoolName string
	// CPUModel contains type (can be overloaded by pod annotation)
	// of cpu model to be passed in libvirt domain definition.
	// Empty value denotes libvirt defaults usage.
	CPUModel string
	// Path to the directory used for shared filesystems
	SharedFilesystemPath string
}

// VirtualizationTool provides methods to operate on libvirt.
type VirtualizationTool struct {
	domainConn    virt.DomainConnection
	storageConn   virt.StorageConnection
	imageManager  ImageManager
	metadataStore metadata.Store
	clock         clockwork.Clock
	volumeSource  VMVolumeSource
	config        VirtualizationConfig
	fsys          fs.FileSystem
	commander     utils.Commander
}

var _ volumeOwner = &VirtualizationTool{}

// NewVirtualizationTool verifies existence of volumes pool in libvirt store
// and returns initialized VirtualizationTool.
func NewVirtualizationTool(domainConn virt.DomainConnection,
	storageConn virt.StorageConnection, imageManager ImageManager,
	metadataStore metadata.Store, volumeSource VMVolumeSource,
	config VirtualizationConfig, fsys fs.FileSystem,
	commander utils.Commander) *VirtualizationTool {
	return &VirtualizationTool{
		domainConn:    domainConn,
		storageConn:   storageConn,
		imageManager:  imageManager,
		metadataStore: metadataStore,
		clock:         clockwork.NewRealClock(),
		volumeSource:  volumeSource,
		config:        config,
		fsys:          fsys,
		commander:     commander,
	}
}

// SetClock sets the clock to use (used in tests)
func (v *VirtualizationTool) SetClock(clock clockwork.Clock) {
	v.clock = clock
}

func (v *VirtualizationTool) addSerialDevicesToDomain(domain *libvirtxml.Domain) error {
	port := uint(0)
	timeout := uint(1)
	if v.config.StreamerSocketPath != "" {
		domain.Devices.Serials = []libvirtxml.DomainSerial{
			{
				Source: &libvirtxml.DomainChardevSource{
					UNIX: &libvirtxml.DomainChardevSourceUNIX{
						Mode: "connect",
						Path: v.config.StreamerSocketPath,
						Reconnect: &libvirtxml.DomainChardevSourceReconnect{
							Enabled: "yes",
							Timeout: &timeout,
						},
					},
				},
				Target: &libvirtxml.DomainSerialTarget{Port: &port},
			},
		}
	} else {
		domain.Devices.Serials = []libvirtxml.DomainSerial{
			{
				Target: &libvirtxml.DomainSerialTarget{Port: &port},
			},
		}
		domain.Devices.Consoles = []libvirtxml.DomainConsole{
			{
				Target: &libvirtxml.DomainConsoleTarget{Type: "serial", Port: &port},
			},
		}
	}
	return nil
}

// CreateContainer defines libvirt domain for VM, prepares it's disks and stores
// all info in metadata store.  It returns domain uuid generated basing on pod
// sandbox id.
func (v *VirtualizationTool) CreateContainer(config *types.VMConfig, netFdKey string) (string, error) {
	if err := config.LoadAnnotations(); err != nil {
		return "", err
	}

	var domainUUID string
	if config.ParsedAnnotations.SystemUUID != nil {
		domainUUID = config.ParsedAnnotations.SystemUUID.String()
	} else {
		domainUUID = utils.NewUUID5(ContainerNsUUID, config.PodSandboxID)
	}
	// FIXME: this field should be moved to VMStatus struct (to be added)
	config.DomainUUID = domainUUID
	cpuModel := v.config.CPUModel
	if config.ParsedAnnotations.CPUModel != "" {
		cpuModel = string(config.ParsedAnnotations.CPUModel)
	}

	settings := domainSettings{
		domainUUID: domainUUID,
		// Note: using only first 13 characters because libvirt has an issue with handling
		// long path names for qemu monitor socket
		domainName:  "virtlet-" + domainUUID[:13] + "-" + config.Name,
		netFdKey:    netFdKey,
		vcpuNum:     uint(config.ParsedAnnotations.VCPUCount),
		memory:      int(config.MemoryLimitInBytes / KiValue),
		memoryUnit:  defaultMemoryUnit,
		cpuShares:   uint(config.CPUShares),
		cpuPeriod:   uint64(config.CPUPeriod),
		enableSriov: v.config.EnableSriov,
		// CPU bandwidth limits for domains are actually set equal per
		// each vCPU by libvirt. Thus, to limit overall VM's CPU
		// threads consumption by the value from the pod definition
		// we need to perform this division
		cpuQuota:   config.CPUQuota / int64(config.ParsedAnnotations.VCPUCount),
		useKvm:     !v.config.DisableKVM,
		cpuModel:   cpuModel,
		systemUUID: config.ParsedAnnotations.SystemUUID,
	}
	if settings.memory == 0 {
		settings.memory = defaultMemory
		settings.memoryUnit = defaultMemoryUnit
	}

	domainDef := settings.createDomain(config)
	diskList, err := newDiskList(config, v.volumeSource, v)
	if err != nil {
		return "", err
	}
	domainDef.Devices.Disks, domainDef.Devices.Filesystems, err = diskList.setup()
	if err != nil {
		return "", err
	}

	ok := false
	defer func() {
		if ok {
			return
		}
		if err := v.removeDomain(settings.domainUUID, config, types.ContainerState_CONTAINER_UNKNOWN, true); err != nil {
			glog.Warningf("Failed to remove domain %q: %v", settings.domainUUID, err)
		}
		if err := diskList.teardown(); err != nil {
			glog.Warningf("error tearing down volumes after an error: %v", err)
		}
	}()

	if err := v.addSerialDevicesToDomain(domainDef); err != nil {
		return "", err
	}

	if config.ContainerLabels == nil {
		config.ContainerLabels = map[string]string{}
	}
	config.ContainerLabels[kubetypes.KubernetesPodNameLabel] = config.PodName
	config.ContainerLabels[kubetypes.KubernetesPodNamespaceLabel] = config.PodNamespace
	config.ContainerLabels[kubetypes.KubernetesPodUIDLabel] = config.PodSandboxID
	config.ContainerLabels[kubetypes.KubernetesContainerNameLabel] = config.Name

	domain, err := v.domainConn.DefineDomain(domainDef)
	if err == nil {
		err = diskList.writeImages(domain)
	}
	if err == nil {
		err = v.metadataStore.Container(settings.domainUUID).Save(
			func(_ *types.ContainerInfo) (*types.ContainerInfo, error) {
				return &types.ContainerInfo{
					Name:      config.Name,
					CreatedAt: v.clock.Now().UnixNano(),
					Config:    *config,
					State:     types.ContainerState_CONTAINER_CREATED,
				}, nil
			})
	}
	if err != nil {
		return "", err
	}

	ok = true
	return settings.domainUUID, nil
}

func (v *VirtualizationTool) startContainer(containerID string) error {
	domain, err := v.domainConn.LookupDomainByUUIDString(containerID)
	if err != nil {
		return fmt.Errorf("failed to look up domain %q: %v", containerID, err)
	}

	state, err := domain.State()
	if err != nil {
		return fmt.Errorf("failed to get state of the domain %q: %v", containerID, err)
	}
	if state != virt.DomainStateShutoff {
		return fmt.Errorf("domain %q: bad state %v upon StartContainer()", containerID, state)
	}

	info, err := v.ContainerInfo(containerID)
	if err != nil {
		glog.Errorf("failed to get containerInfo for container: %v", containerID)
		return err
	}

	// create the cgroup for the qemu process
	//TODO: hugepage setting and match with k8s pod cg property settings, after hugepage is supported in VM type
	var cg containerdCgroups.Cgroup
	if info.Config.CgroupParent != "" {
		cpuShares := uint64(info.Config.CPUShares)
		cg, err = cgroups.CreateChildCgroup(info.Config.CgroupParent, info.Config.DomainUUID, &specs.LinuxResources{
			Memory: &specs.LinuxMemory{Limit: &info.Config.MemoryLimitInBytes},
			CPU:    &specs.LinuxCPU{Shares: &cpuShares, Quota: &info.Config.CPUQuota},
		})

		if err != nil {
			glog.Errorf("failed to create cgroup for domain ID:%v, Name:%v", info.Config.DomainUUID, info.Config.Name)
			return err
		}
	}
	if cg != nil {
		glog.V(4).Infof("cgroup name %v state: %v", info.Config.DomainUUID, cg.State())
	}

	if err = domain.Create(); err != nil {
		if info.Config.CgroupParent != "" {
			cg.Delete()
		}
		return fmt.Errorf("failed to create domain %q: %v", containerID, err)
	}

	// XXX: maybe we don't really have to wait here but I couldn't
	// find it in libvirt docs.
	if err = utils.WaitLoop(func() (bool, error) {
		state, err := domain.State()
		if err != nil {
			return false, fmt.Errorf("failed to get state of the domain %q: %v", containerID, err)
		}
		switch state {
		case virt.DomainStateRunning:
			return true, nil
		case virt.DomainStateShutdown:
			return false, fmt.Errorf("unexpected shutdown for new domain %q", containerID)
		case virt.DomainStateCrashed:
			return false, fmt.Errorf("domain %q crashed on start", containerID)
		default:
			return false, nil
		}
	}, domainStartCheckInterval, domainStartTimeout, v.clock); err != nil {
		return err
	}

	return v.metadataStore.Container(containerID).Save(
		func(c *types.ContainerInfo) (*types.ContainerInfo, error) {
			// make sure the container is not removed during the call
			if c != nil {
				c.State = types.ContainerState_CONTAINER_RUNNING
				c.StartedAt = v.clock.Now().UnixNano()
			}
			return c, nil
		})
}

// StartContainer calls libvirt to start domain, waits up to 10 seconds for
// DOMAIN_RUNNING state, then updates it's state in metadata store.
// If there was an error it will be returned to caller after an domain removal
// attempt.  If also it had an error - both of them will be combined.
func (v *VirtualizationTool) StartContainer(containerID string) error {
	return v.startContainer(containerID)
}

// StopContainer calls graceful shutdown of domain and if it was non successful
// it calls libvirt to destroy that domain.
// Successful shutdown or destroy of domain is followed by removal of
// VM info from metadata store.
// Succeeded removal of metadata is followed by volumes cleanup.
func (v *VirtualizationTool) StopContainer(containerID string, timeout time.Duration) error {
	domain, err := v.domainConn.LookupDomainByUUIDString(containerID)
	if err != nil {
		return err
	}

	// We try to shut down the VM gracefully first. This may take several attempts
	// because shutdown requests may be ignored e.g. when the VM boots.
	// If this fails, we just destroy the domain (i.e. power off the VM).
	err = utils.WaitLoop(func() (bool, error) {
		_, err := v.domainConn.LookupDomainByUUIDString(containerID)
		if err == virt.ErrDomainNotFound {
			return true, nil
		}
		if err != nil {
			return false, fmt.Errorf("failed to look up the domain %q: %v", containerID, err)
		}

		// domain.Shutdown() may return 'invalid operation' error if domain is already
		// shut down. But checking the state beforehand will not make the situation
		// any simpler because we'll still have a race, thus we need multiple attempts
		domainShutdownErr := domain.Shutdown()

		state, err := domain.State()
		if err != nil {
			return false, fmt.Errorf("failed to get state of the domain %q: %v", containerID, err)
		}

		if state == virt.DomainStateShutoff {
			return true, nil
		}

		if domainShutdownErr != nil {
			// The domain is not in 'DOMAIN_SHUTOFF' state and domain.Shutdown() failed,
			// so we need to return the error that happened during Shutdown()
			return false, fmt.Errorf("failed to shut down domain %q: %v", containerID, err)
		}

		return false, nil
	}, domainShutdownRetryInterval, timeout, v.clock)

	if err != nil {
		glog.Warningf("Failed to shut down VM %q: %v -- trying to destroy the domain", containerID, err)
		// if the domain is destroyed successfully we return no error
		if err = domain.Destroy(); err != nil {
			return fmt.Errorf("failed to destroy the domain: %v", err)
		}
	}

	if err == nil {
		err = v.metadataStore.Container(containerID).Save(
			func(c *types.ContainerInfo) (*types.ContainerInfo, error) {
				// make sure the container is not removed during the call
				if c != nil {
					c.State = types.ContainerState_CONTAINER_EXITED
				}
				return c, nil
			})
	}

	if err == nil {
		// Note: volume cleanup is done right after domain has been stopped
		// due to by the time the ContainerRemove request all flexvolume
		// data is already removed by kubelet's VolumeManager
		return v.cleanupVolumes(containerID)
	}

	return err
}

func (v *VirtualizationTool) getVMConfigFromMetadata(containerID string) (*types.VMConfig, types.ContainerState, error) {
	containerInfo, err := v.metadataStore.Container(containerID).Retrieve()
	if err != nil {
		glog.Errorf("Error when retrieving domain %q info from metadata store: %v", containerID, err)
		return nil, types.ContainerState_CONTAINER_UNKNOWN, err
	}
	if containerInfo == nil {
		// the vm is already removed
		return nil, types.ContainerState_CONTAINER_UNKNOWN, nil
	}

	return &containerInfo.Config, containerInfo.State, nil
}

func (v *VirtualizationTool) cleanupVolumes(containerID string) error {
	config, _, err := v.getVMConfigFromMetadata(containerID)
	if err != nil {
		return err
	}

	if config == nil {
		glog.Warningf("No info found for domain %q in metadata store. Volume cleanup skipped.", containerID)
		return nil
	}

	diskList, err := newDiskList(config, v.volumeSource, v)
	if err == nil {
		err = diskList.teardown()
	}

	var errs []string
	if err != nil {
		glog.Errorf("Volume teardown failed for domain %q: %v", containerID, err)
		errs = append(errs, err.Error())
	}

	return nil
}

func (v *VirtualizationTool) removeDomain(containerID string, config *types.VMConfig, state types.ContainerState, failUponVolumeTeardownFailure bool) error {
	// Give a chance to gracefully stop domain
	// TODO: handle errors - there could be e.g. lost connection error
	domain, err := v.domainConn.LookupDomainByUUIDString(containerID)
	if err != nil && err != virt.ErrDomainNotFound {
		return err
	}

	if domain != nil {
		if state == types.ContainerState_CONTAINER_RUNNING {
			if err := domain.Destroy(); err != nil {
				return fmt.Errorf("failed to destroy the domain: %v", err)
			}
		}

		if err := domain.Undefine(); err != nil {
			return fmt.Errorf("error undefining the domain %q: %v", containerID, err)
		}

		// Wait until domain is really removed or timeout after 5 sec.
		if err := utils.WaitLoop(func() (bool, error) {
			if _, err := v.domainConn.LookupDomainByUUIDString(containerID); err == virt.ErrDomainNotFound {
				return true, nil
			} else if err != nil {
				// Unexpected error occurred
				return false, fmt.Errorf("error looking up domain %q: %v", containerID, err)
			}
			return false, nil
		}, domainDestroyCheckInterval, domainDestroyTimeout, v.clock); err != nil {
			return err
		}
	}

	diskList, err := newDiskList(config, v.volumeSource, v)
	if err == nil {
		err = diskList.teardown()
	}

	switch {
	case err == nil:
		return nil
	case failUponVolumeTeardownFailure:
		return err
	default:
		glog.Warningf("Error during volume teardown for container %s: %v", containerID, err)
		return nil
	}
}

// RemoveContainer tries to gracefully stop domain, then forcibly removes it
// even if it's still running.
// It waits up to 5 sec for doing the job by libvirt.
func (v *VirtualizationTool) RemoveContainer(containerID string) error {
	config, state, err := v.getVMConfigFromMetadata(containerID)

	if err != nil {
		return err
	}

	if config == nil {
		glog.Warningf("No info found for domain %q in metadata store. Domain cleanup skipped", containerID)
		return nil
	}

	if err := v.removeDomain(containerID, config, state, state == types.ContainerState_CONTAINER_CREATED ||
		state == types.ContainerState_CONTAINER_RUNNING); err != nil {
		return err
	}

	if v.metadataStore.Container(containerID).Save(
		func(_ *types.ContainerInfo) (*types.ContainerInfo, error) {
			return nil, nil // delete container
		},
	); err != nil {
		glog.Errorf("Error when removing container '%s' from metadata store: %v", containerID, err)
		return err
	}

	return nil
}

func virtToKubeState(domainState virt.DomainState, lastState types.ContainerState) types.ContainerState {
	var containerState types.ContainerState

	switch domainState {
	case virt.DomainStateShutdown:
		// the domain is being shut down, but is still running
		fallthrough
	case virt.DomainStateRunning:
		containerState = types.ContainerState_CONTAINER_RUNNING
	case virt.DomainStatePaused:
		if lastState == types.ContainerState_CONTAINER_CREATED {
			containerState = types.ContainerState_CONTAINER_CREATED
		} else {
			containerState = types.ContainerState_CONTAINER_EXITED
		}
	case virt.DomainStateShutoff:
		if lastState == types.ContainerState_CONTAINER_CREATED {
			containerState = types.ContainerState_CONTAINER_CREATED
		} else {
			containerState = types.ContainerState_CONTAINER_EXITED
		}
	case virt.DomainStateCrashed:
		containerState = types.ContainerState_CONTAINER_EXITED
	case virt.DomainStatePMSuspended:
		containerState = types.ContainerState_CONTAINER_EXITED
	default:
		containerState = types.ContainerState_CONTAINER_UNKNOWN
	}

	return containerState
}

func (v *VirtualizationTool) getPodContainer(podSandboxID string) (*types.ContainerInfo, error) {
	// FIXME: is it possible for multiple containers to exist?
	domainContainers, err := v.metadataStore.ListPodContainers(podSandboxID)
	if err != nil {
		// There's no such sandbox. Looks like it's already removed, so return an empty list
		return nil, nil
	}
	for _, containerMeta := range domainContainers {
		// TODO: Distinguish lack of domain from other errors
		_, err := v.domainConn.LookupDomainByUUIDString(containerMeta.GetID())
		if err != nil {
			// There's no such domain. Looks like it's already removed, so return an empty list
			return nil, nil
		}

		// Verify if there is container metadata
		containerInfo, err := containerMeta.Retrieve()
		if err != nil {
			return nil, err
		}
		if containerInfo == nil {
			// There's no such container - looks like it's already removed, but still is mentioned in sandbox
			return nil, fmt.Errorf("container metadata not found, but it's still mentioned in sandbox %s", podSandboxID)
		}

		return containerInfo, nil
	}
	return nil, nil
}

// ListContainers queries libvirt for domains denoted by container id or
// pod standbox id or for all domains and after gathering theirs description
// from metadata and conversion of status from libvirt to kubeapi compatible
// returns them as a list of kubeapi Containers.
func (v *VirtualizationTool) ListContainers(filter *types.ContainerFilter) ([]*types.ContainerInfo, error) {
	var containers []*types.ContainerInfo
	switch {
	case filter != nil && filter.Id != "":
		containerInfo, err := v.ContainerInfo(filter.Id)
		if err != nil || containerInfo == nil {
			return nil, err
		}
		containers = append(containers, containerInfo)
	case filter != nil && filter.PodSandboxID != "":
		containerInfo, err := v.getPodContainer(filter.PodSandboxID)
		if err != nil || containerInfo == nil {
			return nil, err
		}
		containers = append(containers, containerInfo)
	default:
		// Get list of all the defined domains from libvirt
		// and check each container against the remaining
		// filter settings
		domains, err := v.domainConn.ListDomains()
		if err != nil {
			return nil, err
		}
		for _, domain := range domains {
			containerID, err := domain.UUIDString()
			if err != nil {
				return nil, err
			}
			containerInfo, err := v.ContainerInfo(containerID)
			if err != nil {
				return nil, err
			}

			if containerInfo == nil {
				glog.V(1).Infof("Failed to find info for domain with id %q in virtlet db, considering it a non-virtlet libvirt domain.", containerID)
				continue
			}
			containers = append(containers, containerInfo)
		}
	}

	if filter == nil {
		return containers, nil
	}

	var r []*types.ContainerInfo
	for _, c := range containers {
		if filterContainer(c, *filter) {
			r = append(r, c)
		}
	}

	return r, nil
}

// ContainerInfo returns info for the specified container, making sure it's also
// present among libvirt domains. If it isn't, the function returns nil
func (v *VirtualizationTool) ContainerInfo(containerID string) (*types.ContainerInfo, error) {
	domain, err := v.domainConn.LookupDomainByUUIDString(containerID)
	if err != nil {
		return nil, err
	}

	containerInfo, err := v.metadataStore.Container(containerID).Retrieve()
	if err != nil {
		return nil, err
	}
	if containerInfo == nil {
		return nil, nil
	}

	state, err := domain.State()
	if err != nil {
		return nil, err
	}

	containerState := virtToKubeState(state, containerInfo.State)
	if containerInfo.State != containerState {
		if err := v.metadataStore.Container(containerID).Save(
			func(c *types.ContainerInfo) (*types.ContainerInfo, error) {
				// make sure the container is not removed during the call
				if c != nil {
					c.State = containerState
				}
				return c, nil
			},
		); err != nil {
			return nil, err
		}
		containerInfo.State = containerState
	}
	return containerInfo, nil
}

// VMStats returns current cpu/memory/disk usage for VM
func (v *VirtualizationTool) VMStats(containerID string, name string) (*types.VMStats, error) {
	domain, err := v.domainConn.LookupDomainByUUIDString(containerID)
	if err != nil {
		return nil, err
	}
	vs := types.VMStats{
		Timestamp:   v.clock.Now().UnixNano(),
		ContainerID: containerID,
		Name:        name,
	}

	rss, err := domain.GetRSS()
	if err != nil {
		return nil, err
	}
	vs.MemoryUsage = rss

	cpuTime, err := domain.GetCPUTime()
	if err != nil {
		return nil, err
	}
	vs.CpuUsage = cpuTime

	domainxml, err := domain.XML()
	if err != nil {
		return nil, err
	}

	rootDiskLocation := ""
	for _, disk := range domainxml.Devices.Disks {
		if disk.Source == nil || disk.Source.File == nil {
			continue
		}
		fname := disk.Source.File.File
		// TODO: split file name and use HasPrefix on last part
		// instead of Contains
		if strings.Contains(fname, "virtlet_root_") {
			rootDiskLocation = fname
		}
	}
	if rootDiskLocation == "" {
		return nil, fmt.Errorf("cannot locate root disk in domain definition")
	}

	rootDiskSize, err := v.ImageManager().BytesUsedBy(rootDiskLocation)
	if err != nil {
		return nil, err
	}
	vs.FsBytes = rootDiskSize

	glog.V(4).Infof("VMStats - cpu: %d, mem: %d, disk: %d, timestamp: %d", vs.CpuUsage, vs.MemoryUsage, vs.FsBytes, vs.Timestamp)

	return &vs, nil
}

// ListVMStats returns statistics (same as VMStats) for all containers matching
// provided filter (id AND podstandboxid AND labels)
func (v *VirtualizationTool) ListVMStats(filter *types.VMStatsFilter) ([]types.VMStats, error) {
	var containersFilter *types.ContainerFilter
	if filter != nil {
		containersFilter = &types.ContainerFilter{}
		if filter.Id != "" {
			containersFilter.Id = filter.Id
		}
		if filter.PodSandboxID != "" {
			containersFilter.PodSandboxID = filter.PodSandboxID
		}
		if filter.LabelSelector != nil {
			containersFilter.LabelSelector = filter.LabelSelector
		}
	}

	infos, err := v.ListContainers(containersFilter)
	if err != nil {
		return nil, err
	}

	var statsList []types.VMStats
	for _, info := range infos {
		stats, err := v.VMStats(info.Id, info.Name)
		if err != nil {
			return nil, err
		}
		statsList = append(statsList, *stats)
	}
	return statsList, nil
}

// volumeOwner implementation follows

// StoragePool implements volumeOwner StoragePool method
func (v *VirtualizationTool) StoragePool() (virt.StoragePool, error) {
	return ensureStoragePool(v.storageConn, v.config.VolumePoolName)
}

// DomainConnection implements volumeOwner DomainConnection method
func (v *VirtualizationTool) DomainConnection() virt.DomainConnection { return v.domainConn }

// StorageConnection implements volumeOwner StorageConnection method
func (v *VirtualizationTool) StorageConnection() virt.StorageConnection { return v.storageConn }

// ImageManager implements volumeOwner ImageManager method
func (v *VirtualizationTool) ImageManager() ImageManager { return v.imageManager }

// RawDevices implements volumeOwner RawDevices method
func (v *VirtualizationTool) RawDevices() []string { return v.config.RawDevices }

// KubeletRootDir implements volumeOwner KubeletRootDir method
func (v *VirtualizationTool) KubeletRootDir() string { return v.config.KubeletRootDir }

// VolumePoolName implements volumeOwner VolumePoolName method
func (v *VirtualizationTool) VolumePoolName() string { return v.config.VolumePoolName }

// FileSystem implements volumeOwner FileSystem method
func (v *VirtualizationTool) FileSystem() fs.FileSystem { return v.fsys }

// SharedFilesystemPath implements volumeOwner SharedFilesystemPath method
func (v *VirtualizationTool) SharedFilesystemPath() string { return v.config.SharedFilesystemPath }

// Commander implements volumeOwner Commander method
func (v *VirtualizationTool) Commander() utils.Commander { return v.commander }

func filterContainer(containerInfo *types.ContainerInfo, filter types.ContainerFilter) bool {
	if filter.Id != "" && containerInfo.Id != filter.Id {
		return false
	}

	if filter.PodSandboxID != "" && containerInfo.Config.PodSandboxID != filter.PodSandboxID {
		return false
	}

	if filter.State != nil && containerInfo.State != *filter.State {
		return false
	}
	if filter.LabelSelector != nil {
		sel := fields.SelectorFromSet(filter.LabelSelector)
		if !sel.Matches(fields.Set(containerInfo.Config.ContainerLabels)) {
			return false
		}
	}

	return true
}

func (v *VirtualizationTool) RebootVM(vmID string) error {
	domain, err := v.domainConn.LookupDomainByUUIDString(vmID)
	if err != nil {
		return err
	}

	// TODO: fix to use the flag from RebootVmRequest
	// just take the default for now
	// https://libvirt.org/html/libvirt-libvirt-domain.html#virDomainRebootFlagValues
	domain.Reboot(0)

	return nil
}

func (v *VirtualizationTool) CreateSnapshot(vmID string, snapshotID string) error {
	domain, err := v.domainConn.LookupDomainByUUIDString(vmID)
	if err != nil {
		return err
	}

	return domain.CreateSnapshot(snapshotID)
}

func (v *VirtualizationTool) RestoreToSnapshot(vmID string, snapshotID string) error {
	domain, err := v.domainConn.LookupDomainByUUIDString(vmID)
	if err != nil {
		return err
	}

	return domain.RestoreToSnapshot(snapshotID)
}

// Live update the VM compute resources
func (v *VirtualizationTool) UpdateDomainResources(vmID string, lcr *kubeapi.LinuxContainerResources) error {
	glog.V(4).Infof("Update Domain Resources %v, %v", vmID, lcr)

	domain, err := v.domainConn.LookupDomainByUUIDString(vmID)
	if err != nil {
		return err
	}

	// update the vcpu count
	domainXml, err := domain.XML()
	if err != nil {
		return err
	}

	var cpuUpdated bool
	//var memUpated bool

	// Update vcpus if needed, this is the reversed calculation from the Agent side
	requestedVcpus := v.getVcpusInRequest(lcr)
	glog.V(5).Infof("Update domain vCPU number: %v -> %v", domainXml.VCPU.Value, requestedVcpus)
	if requestedVcpus != int64(domainXml.VCPU.Value) {
		err := domain.SetVcpus(uint(requestedVcpus))
		if err != nil {
			return err
		}
		cpuUpdated = true
	}

	// Update the memory
	currentMemory := domainXml.CurrentMemory.Value
	newmemory := lcr.MemoryLimitInBytes / int64(defaultLibvirtDomainMemoryUnitValue)

	if newmemory != int64(currentMemory) {
		domain.SetCurrentMemory(uint64(newmemory))
	}

	// TODO: Update the vm config and metadata stored in Arktos-vm-runtime metadata
	containerInfo, err := v.metadataStore.Container(vmID).Retrieve()
	if err != nil {
		return err
	}

	if cpuUpdated {
		containerInfo.Config.CPUShares = lcr.CpuShares
		containerInfo.Config.CPUQuota = lcr.CpuQuota
		containerInfo.Config.CPUPeriod = lcr.CpuPeriod
	}

	//if memUpdated {
	//	containerInfo.Config.MemoryLimitInBytes = *lcr.Memory.Limit
	//}
	//
	err = v.metadataStore.Container(vmID).Save(
		func(_ *types.ContainerInfo) (*types.ContainerInfo, error) {
			return containerInfo, nil
		})

	if err != nil {
		glog.Errorf("failed to save containerInfo for container: %v", vmID)
		return err
	}

	return nil
}

// ensure the containerInfo.Config in-sync with the domain xml
// in UpdateContainerResources() API, updateDomainResource function updates both VMconfig in the containerInfo and in libvirt VM domain
// during runtime exe crash or other error situation, there exists cases the two set of VM metadaata is out of sync
// since the libvirt domain were updated first, so keep the domain xml as source of truth
func (v *VirtualizationTool) SyncContainerInfoWithLibvirtDomain(vmID string) (*types.ContainerInfo, error) {
	glog.V(4).Infof("Sync vm config with libvirt domain settings for VM: %v", vmID)

	mem, memUnit, cpuShares, cpuQuota, cpuPeriod, _, err := v.GetDomainConfigredResources(vmID)
	if err != nil {
		return nil, err
	}

	if memUnit != defaultMemoryUnit && memUnit != "K" {
		glog.V(5).Infof("domain info: memory: %v, memoryUnit: %v, cpuShares: %v, cpuQuota: %v, cpuPeriod: %v",
			mem, memUnit, cpuShares, cpuQuota, cpuPeriod)
		return nil, fmt.Errorf("unexpected Domain MemoryUnit: %v", memUnit)
	}

	containerInfo, err := v.metadataStore.Container(vmID).Retrieve()
	if err != nil {
		return nil, err
	}

	configuredMem := containerInfo.Config.MemoryLimitInBytes / KiValue

	needSync := false

	// always compare with the bigger memUnit to void false positive
	// due to reduced precision lose errors in bytes-mib conversions
	if configuredMem != mem {
		containerInfo.Config.MemoryLimitInBytes = mem * KiValue
		needSync = true
	}

	if containerInfo.Config.CPUPeriod != cpuPeriod {
		containerInfo.Config.CPUPeriod = cpuPeriod
		needSync = true
	}

	if containerInfo.Config.CPUQuota != cpuQuota {
		containerInfo.Config.CPUQuota = cpuQuota
		needSync = true
	}

	if containerInfo.Config.CPUShares != cpuShares {
		containerInfo.Config.CPUShares = cpuShares
		needSync = true
	}

	if !needSync {
		return containerInfo, nil
	}

	err = v.metadataStore.Container(vmID).Save(
		func(_ *types.ContainerInfo) (*types.ContainerInfo, error) {
			return containerInfo, nil
		})

	if err != nil {
		glog.Errorf("failed to save containerInfo for container: %v", vmID)
		return nil, err
	}

	return containerInfo, nil

}

func (v *VirtualizationTool) GetDomainConfigredResources(vmID string) (int64, string, int64, int64, int64, int, error) {
	domain, err := v.domainConn.LookupDomainByUUIDString(vmID)
	if err != nil {
		return 0, "", 0, 0, 0, 0, err
	}

	domainXml, err := domain.XML()
	if err != nil {
		return 0, "", 0, 0, 0, 0, err
	}

	mem := domainXml.Memory.Value
	memUnit := domainXml.Memory.Unit

	cpuShares := domainXml.CPUTune.Shares.Value
	cpuQuotas := domainXml.CPUTune.Quota.Value
	cpuPeriod := domainXml.CPUTune.Period.Value
	vCpus := domainXml.VCPU.Current
	return int64(mem), memUnit, int64(cpuShares), int64(cpuQuotas), int64(cpuPeriod), int(vCpus), nil

}

// essentially the reversed calculation in Kubelet to construct the UpdateContainerRequest
func (v *VirtualizationTool) getVcpusInRequest(lcr *kubeapi.LinuxContainerResources) int64 {
	return lcr.CpuQuota / lcr.CpuPeriod
}
