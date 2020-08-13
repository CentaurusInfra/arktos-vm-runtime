/*
Copyright 2016-2018 Mirantis
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

package manager

import (
	"errors"
	"fmt"
	"github.com/Mirantis/virtlet/pkg/utils/cgroups"
	"github.com/opencontainers/runtime-spec/specs-go"
	"path"
	"strings"
	"time"

	cnitypes "github.com/containernetworking/cni/pkg/types"
	cnicurrent "github.com/containernetworking/cni/pkg/types/current"
	"github.com/golang/glog"
	"github.com/jonboulle/clockwork"
	"golang.org/x/net/context"
	kubeapi "k8s.io/kubernetes/pkg/kubelet/apis/cri/runtime/v1alpha2"

	"github.com/Mirantis/virtlet/pkg/cni"
	"github.com/Mirantis/virtlet/pkg/libvirttools"
	"github.com/Mirantis/virtlet/pkg/metadata"
	"github.com/Mirantis/virtlet/pkg/metadata/types"
	"github.com/Mirantis/virtlet/pkg/tapmanager"
)

const (
	runtimeAPIVersion = "0.1.0"
	runtimeName       = "virtlet"
	runtimeVersion    = "0.1.0"
)

// StreamServer denotes a server that handles Attach and PortForward requests.
type StreamServer interface {
	GetAttach(req *kubeapi.AttachRequest) (*kubeapi.AttachResponse, error)
	GetPortForward(req *kubeapi.PortForwardRequest) (*kubeapi.PortForwardResponse, error)
}

// GCHandler performs GC when a container is deleted.
type GCHandler interface {
	GC() error
}

// VirtletRuntimeService handles CRI runtime service calls.
type VirtletRuntimeService struct {
	virtTool      *libvirttools.VirtualizationTool
	metadataStore metadata.Store
	fdManager     tapmanager.FDManager
	streamServer  StreamServer
	gcHandler     GCHandler
	clock         clockwork.Clock
}

// NewVirtletRuntimeService returns a new instance of VirtletRuntimeService.
func NewVirtletRuntimeService(
	virtTool *libvirttools.VirtualizationTool,
	metadataStore metadata.Store,
	fdManager tapmanager.FDManager,
	streamServer StreamServer,
	gcHandler GCHandler,
	clock clockwork.Clock) *VirtletRuntimeService {
	if clock == nil {
		clock = clockwork.NewRealClock()
	}
	return &VirtletRuntimeService{
		virtTool:      virtTool,
		metadataStore: metadataStore,
		fdManager:     fdManager,
		streamServer:  streamServer,
		gcHandler:     gcHandler,
		clock:         clock,
	}
}

// Version implements Version method of CRI.
func (v *VirtletRuntimeService) Version(ctx context.Context, in *kubeapi.VersionRequest) (*kubeapi.VersionResponse, error) {
	vRuntimeAPIVersion := runtimeAPIVersion
	vRuntimeName := runtimeName
	vRuntimeVersion := runtimeVersion
	return &kubeapi.VersionResponse{
		Version:           vRuntimeAPIVersion,
		RuntimeName:       vRuntimeName,
		RuntimeVersion:    vRuntimeVersion,
		RuntimeApiVersion: vRuntimeVersion,
	}, nil
}

//
// Sandboxes
//

// RunPodSandbox implements RunPodSandbox method of CRI.
func (v *VirtletRuntimeService) RunPodSandbox(ctx context.Context, in *kubeapi.RunPodSandboxRequest) (response *kubeapi.RunPodSandboxResponse, retErr error) {
	config := in.GetConfig()
	if config == nil {
		return nil, errors.New("no pod sandbox config passed to RunPodSandbox")
	}
	podName := "<no metadata>"
	if config.Metadata != nil {
		podName = config.Metadata.Name
	}
	if err := validatePodSandboxConfig(config); err != nil {
		return nil, err
	}
	podID := config.Metadata.Uid
	podNs := config.Metadata.Namespace
	podTenant := config.Metadata.Tenant

	// Check if sandbox already exists, it may happen when virtlet restarts and kubelet "thinks" that sandbox disappered
	sandbox := v.metadataStore.PodSandbox(podID)
	sandboxInfo, err := sandbox.Retrieve()
	if err == nil && sandboxInfo != nil {
		if sandboxInfo.State == types.PodSandboxState_SANDBOX_READY {
			return &kubeapi.RunPodSandboxResponse{
				PodSandboxId: podID,
			}, nil
		}
	}

	state := kubeapi.PodSandboxState_SANDBOX_READY
	pnd := &tapmanager.PodNetworkDesc{
		PodID:     podID,
		PodTenant: podTenant,
		PodNs:     podNs,
		PodName:   podName,
		VPC:       config.Annotations["VPC"],
		NICs:      config.Annotations["NICs"],
		CNIArgs:   config.Annotations["arktos.futurewei.com/cni-args"],
	}
	// Mimic kubelet's method of handling nameservers.
	// As of k8s 1.5.2, kubelet doesn't use any nameserver information from CNI.
	// (TODO: recheck this for 1.6)
	// CNI is used just to configure the network namespace and CNI DNS
	// info is ignored. Instead of this, DnsConfig from PodSandboxConfig
	// is used to configure container's resolv.conf.
	if config.DnsConfig != nil {
		pnd.DNS = &cnitypes.DNS{
			Nameservers: config.DnsConfig.Servers,
			Search:      config.DnsConfig.Searches,
			Options:     config.DnsConfig.Options,
		}
	}

	fdPayload := &tapmanager.GetFDPayload{Description: pnd}
	csnBytes, err := v.fdManager.AddFDs(podID, fdPayload)
	// The reason for defer here is that it is also necessary to ReleaseFDs if AddFDs fail
	// Try to clean up CNI netns (this may be necessary e.g. in case of multiple CNI plugins with CNI Genie)
	defer func() {
		if retErr != nil {
			// Try to clean up CNI netns if we couldn't add the pod to the metadata store or if AddFDs call wasn't
			// successful to avoid leaking resources
			if fdErr := v.fdManager.ReleaseFDs(podID); fdErr != nil {
				glog.Errorf("Error removing pod %s (%s) from CNI network: %v", podName, podID, fdErr)
			}
		}
	}()
	if err != nil {
		return nil, fmt.Errorf("Error adding pod %s (%s) to CNI network: %v", podName, podID, err)
	}

	psi, err := metadata.NewPodSandboxInfo(
		CRIPodSandboxConfigToPodSandboxConfig(config),
		csnBytes, types.PodSandboxState(state), v.clock)
	if err != nil {
		return nil, err
	}

	glog.V(5).Infof("PodSandBoxInfo Pod cgroup parent: %v", psi.Config.CgroupParent)

	sandbox = v.metadataStore.PodSandbox(config.Metadata.Uid)
	if err := sandbox.Save(
		func(c *types.PodSandboxInfo) (*types.PodSandboxInfo, error) {
			return psi, nil
		},
	); err != nil {
		return nil, err
	}

	return &kubeapi.RunPodSandboxResponse{
		PodSandboxId: podID,
	}, nil
}

// StopPodSandbox implements StopPodSandbox method of CRI.
func (v *VirtletRuntimeService) StopPodSandbox(ctx context.Context, in *kubeapi.StopPodSandboxRequest) (*kubeapi.StopPodSandboxResponse, error) {
	sandbox := v.metadataStore.PodSandbox(in.PodSandboxId)
	switch sandboxInfo, err := sandbox.Retrieve(); {
	case err != nil:
		return nil, err
	case sandboxInfo == nil:
		return nil, fmt.Errorf("sandbox %q not found in Virtlet metadata store", in.PodSandboxId)
	// check if the sandbox is already stopped
	case sandboxInfo.State != types.PodSandboxState_SANDBOX_NOTREADY:
		if err := sandbox.Save(
			func(c *types.PodSandboxInfo) (*types.PodSandboxInfo, error) {
				// make sure the pod is not removed during the call
				if c != nil {
					c.State = types.PodSandboxState_SANDBOX_NOTREADY
				}
				return c, nil
			},
		); err != nil {
			return nil, err
		}

		if err := v.fdManager.ReleaseFDs(in.PodSandboxId); err != nil {
			glog.Errorf("Error releasing tap fd for the pod %q: %v", in.PodSandboxId, err)
		}
	}

	response := &kubeapi.StopPodSandboxResponse{}
	return response, nil
}

// RemovePodSandbox method implements RemovePodSandbox from CRI.
func (v *VirtletRuntimeService) RemovePodSandbox(ctx context.Context, in *kubeapi.RemovePodSandboxRequest) (*kubeapi.RemovePodSandboxResponse, error) {
	podSandboxID := in.PodSandboxId

	if err := v.metadataStore.PodSandbox(podSandboxID).Save(
		func(c *types.PodSandboxInfo) (*types.PodSandboxInfo, error) {
			return nil, nil
		},
	); err != nil {
		return nil, err
	}

	response := &kubeapi.RemovePodSandboxResponse{}
	return response, nil
}

// PodSandboxStatus method implements PodSandboxStatus from CRI.
func (v *VirtletRuntimeService) PodSandboxStatus(ctx context.Context, in *kubeapi.PodSandboxStatusRequest) (*kubeapi.PodSandboxStatusResponse, error) {
	podSandboxID := in.PodSandboxId

	sandbox := v.metadataStore.PodSandbox(podSandboxID)
	sandboxInfo, err := sandbox.Retrieve()
	if err != nil {
		return nil, err
	}
	if sandboxInfo == nil {
		return nil, fmt.Errorf("sandbox %q not found in Virtlet metadata store", podSandboxID)
	}
	status := PodSandboxInfoToCRIPodSandboxStatus(sandboxInfo)

	var cniResult *cnicurrent.Result
	if sandboxInfo.ContainerSideNetwork != nil {
		cniResult = sandboxInfo.ContainerSideNetwork.Result
	}

	ip := cni.GetPodIP(cniResult)
	if ip != "" {
		status.Network = &kubeapi.PodSandboxNetworkStatus{Ip: ip}
	}

	response := &kubeapi.PodSandboxStatusResponse{Status: status}
	return response, nil
}

// ListPodSandbox method implements ListPodSandbox from CRI.
func (v *VirtletRuntimeService) ListPodSandbox(ctx context.Context, in *kubeapi.ListPodSandboxRequest) (*kubeapi.ListPodSandboxResponse, error) {
	filter := CRIPodSandboxFilterToPodSandboxFilter(in.GetFilter())
	sandboxes, err := v.metadataStore.ListPodSandboxes(filter)
	if err != nil {
		return nil, err
	}
	var podSandboxList []*kubeapi.PodSandbox
	for _, sandbox := range sandboxes {
		sandboxInfo, err := sandbox.Retrieve()
		if err != nil {
			glog.Errorf("Error retrieving pod sandbox %q", sandbox.GetID())
		}
		if sandboxInfo != nil {
			podSandboxList = append(podSandboxList, PodSandboxInfoToCRIPodSandbox(sandboxInfo))
		}
	}
	response := &kubeapi.ListPodSandboxResponse{Items: podSandboxList}
	return response, nil
}

//
// Containers
//

// CreateContainer method implements CreateContainer from CRI.
func (v *VirtletRuntimeService) CreateContainer(ctx context.Context, in *kubeapi.CreateContainerRequest) (*kubeapi.CreateContainerResponse, error) {
	config := in.GetConfig()
	podSandboxID := in.PodSandboxId
	name := config.GetMetadata().Name

	// Was a container already started in this sandbox?
	// NOTE: there is no distinction between lack of key and other types of
	// errors when accessing boltdb. This will be changed when we switch to
	// storing whole marshaled sandbox metadata as json.
	remainingContainers, err := v.metadataStore.ListPodContainers(podSandboxID)
	if err != nil {
		glog.V(3).Infof("Error retrieving pod %q containers", podSandboxID)
	} else {
		for _, container := range remainingContainers {
			glog.V(3).Infof("CreateContainer: there's already a container in the sandbox (id: %s)", container.GetID())
			response := &kubeapi.CreateContainerResponse{ContainerId: container.GetID()}
			return response, nil
		}
	}

	sandboxInfo, err := v.metadataStore.PodSandbox(podSandboxID).Retrieve()
	if err != nil {
		return nil, err
	}
	if sandboxInfo == nil {
		return nil, fmt.Errorf("sandbox %q not in Virtlet metadata store", podSandboxID)
	}

	fdKey := podSandboxID
	vmConfig, err := GetVMConfig(in, sandboxInfo.ContainerSideNetwork)
	if err != nil {
		return nil, err
	}
	if sandboxInfo.ContainerSideNetwork == nil || sandboxInfo.ContainerSideNetwork.Result == nil {
		fdKey = ""
	}

	vmConfig.CgroupParent = sandboxInfo.Config.CgroupParent

	uuid, err := v.virtTool.CreateContainer(vmConfig, fdKey)
	if err != nil {
		glog.Errorf("Error creating container %s: %v", name, err)
		return nil, err
	}

	response := &kubeapi.CreateContainerResponse{ContainerId: uuid}
	return response, nil
}

// StartContainer method implements StartContainer from CRI.
func (v *VirtletRuntimeService) StartContainer(ctx context.Context, in *kubeapi.StartContainerRequest) (*kubeapi.StartContainerResponse, error) {
	info, err := v.virtTool.ContainerInfo(in.ContainerId)
	if err == nil && info != nil && info.State == types.ContainerState_CONTAINER_RUNNING {
		glog.V(2).Infof("StartContainer: Container %s is already running", in.ContainerId)
		response := &kubeapi.StartContainerResponse{}
		return response, nil
	}

	if err := v.virtTool.StartContainer(in.ContainerId); err != nil {
		return nil, err
	}
	response := &kubeapi.StartContainerResponse{}
	return response, nil
}

// StopContainer method implements StopContainer from CRI.
func (v *VirtletRuntimeService) StopContainer(ctx context.Context, in *kubeapi.StopContainerRequest) (*kubeapi.StopContainerResponse, error) {
	if err := v.virtTool.StopContainer(in.ContainerId, time.Duration(in.Timeout)*time.Second); err != nil {
		return nil, err
	}
	response := &kubeapi.StopContainerResponse{}
	return response, nil
}

// RemoveContainer method implements RemoveContainer from CRI.
func (v *VirtletRuntimeService) RemoveContainer(ctx context.Context, in *kubeapi.RemoveContainerRequest) (*kubeapi.RemoveContainerResponse, error) {
	if err := v.virtTool.RemoveContainer(in.ContainerId); err != nil {
		return nil, err
	}

	if err := v.gcHandler.GC(); err != nil {
		return nil, fmt.Errorf("GC error: %v", err)
	}

	response := &kubeapi.RemoveContainerResponse{}
	return response, nil
}

// ListContainers method implements ListContainers from CRI.
func (v *VirtletRuntimeService) ListContainers(ctx context.Context, in *kubeapi.ListContainersRequest) (*kubeapi.ListContainersResponse, error) {
	filter := CRIContainerFilterToContainerFilter(in.GetFilter())
	containers, err := v.virtTool.ListContainers(filter)
	if err != nil {
		return nil, err
	}
	var r []*kubeapi.Container
	for _, c := range containers {
		r = append(r, ContainerInfoToCRIContainer(c))
	}
	response := &kubeapi.ListContainersResponse{Containers: r}
	return response, nil
}

// ContainerStatus method implements ContainerStatus from CRI.
func (v *VirtletRuntimeService) ContainerStatus(ctx context.Context, in *kubeapi.ContainerStatusRequest) (*kubeapi.ContainerStatusResponse, error) {
	//TODO: consider add a lock to avoid the Sync function runs during the UpdateContainerResource
	info, err := v.virtTool.SyncContainerInfoWithLibvirtDomain(in.ContainerId)
	if err != nil {
		return nil, err
	}

	response := &kubeapi.ContainerStatusResponse{Status: ContainerInfoToCRIContainerStatus(info)}
	return response, nil
}

// ExecSync is a placeholder for an unimplemented CRI method.
func (v *VirtletRuntimeService) ExecSync(context.Context, *kubeapi.ExecSyncRequest) (*kubeapi.ExecSyncResponse, error) {
	return nil, errors.New("not implemented")
}

// Exec is a placeholder for an unimplemented CRI method.
func (v *VirtletRuntimeService) Exec(context.Context, *kubeapi.ExecRequest) (*kubeapi.ExecResponse, error) {
	return nil, errors.New("not implemented")
}

// Attach calls streamer server to implement Attach functionality from CRI.
func (v *VirtletRuntimeService) Attach(ctx context.Context, req *kubeapi.AttachRequest) (*kubeapi.AttachResponse, error) {
	if !req.Stdout && !req.Stderr {
		// Support k8s 1.8 or earlier.
		// We don't care about Stderr because it's not used
		// by the Virtlet stream server.
		req.Stdout = true
	}
	return v.streamServer.GetAttach(req)
}

// PortForward calls streamer server to implement PortForward functionality from CRI.
func (v *VirtletRuntimeService) PortForward(ctx context.Context, req *kubeapi.PortForwardRequest) (*kubeapi.PortForwardResponse, error) {
	return v.streamServer.GetPortForward(req)
}

// UpdateRuntimeConfig is a placeholder for an unimplemented CRI method.
func (v *VirtletRuntimeService) UpdateRuntimeConfig(context.Context, *kubeapi.UpdateRuntimeConfigRequest) (*kubeapi.UpdateRuntimeConfigResponse, error) {
	// we don't need to do anything here for now
	return &kubeapi.UpdateRuntimeConfigResponse{}, nil
}

// UpdateContainerResources stores in domain on libvirt info about Cpuset
// for container then looks for running emulator and tries to adjust its
// current settings through cgroups
func (v *VirtletRuntimeService) UpdateContainerResources(ctx context.Context, req *kubeapi.UpdateContainerResourcesRequest) (*kubeapi.UpdateContainerResourcesResponse, error) {
	glog.V(4).Infof("Update Container Resources : %v", req)
	lcr := req.GetLinux()
	if lcr == nil {
		glog.Errorf("linuxContainerResource is not set in UpdateContainerResourceRequest")
		return &kubeapi.UpdateContainerResourcesResponse{}, nil
	}

	lr := linuxContinerResourceToLinuxResource(lcr)

	containerId := req.ContainerId
	info, err := v.virtTool.ContainerInfo(containerId)
	if err != nil {
		return nil, err
	}

	err = cgroups.UpdateVmCgroup(path.Join(info.Config.CgroupParent, containerId), lr)
	if err != nil {
		return nil, err
	}

	// TODO: should revert the CG update if the update domain resource function failed
	err = v.virtTool.UpdateDomainResources(containerId, lcr)
	if err != nil {
		glog.V(4).Infof("Update Domain Resource failed with error: %v", err)
		return nil, err
	}

	return &kubeapi.UpdateContainerResourcesResponse{}, nil
}

func linuxContinerResourceToLinuxResource(lcr *kubeapi.LinuxContainerResources) *specs.LinuxResources {
	cpuShares := uint64(lcr.CpuShares)
	cpuPeriod := uint64(lcr.CpuPeriod)

	return &specs.LinuxResources{
		Memory: &specs.LinuxMemory{Limit: &lcr.MemoryLimitInBytes},
		CPU: &specs.LinuxCPU{Shares: &cpuShares,
			Quota:  &lcr.CpuQuota,
			Period: &cpuPeriod,
		},
	}
}

// Status method implements Status from CRI for both types of service, Image and Runtime.
func (v *VirtletRuntimeService) Status(context.Context, *kubeapi.StatusRequest) (*kubeapi.StatusResponse, error) {
	ready := true
	runtimeReadyStr := kubeapi.RuntimeReady
	networkReadyStr := kubeapi.NetworkReady
	return &kubeapi.StatusResponse{
		Status: &kubeapi.RuntimeStatus{
			Conditions: []*kubeapi.RuntimeCondition{
				{
					Type:   runtimeReadyStr,
					Status: ready,
				},
				{
					Type:   networkReadyStr,
					Status: ready,
				},
			},
		},
	}, nil
}

// ContainerStats returns cpu/memory/disk usage for particular container id
func (v *VirtletRuntimeService) ContainerStats(ctx context.Context, in *kubeapi.ContainerStatsRequest) (*kubeapi.ContainerStatsResponse, error) {
	info, err := v.virtTool.ContainerInfo(in.ContainerId)
	if err != nil {
		return nil, err
	}
	vs, err := v.virtTool.VMStats(info.Id, info.Name)
	if err != nil {
		return nil, err
	}
	fsstats, err := v.virtTool.ImageManager().FilesystemStats()
	if err != nil {
		return nil, err
	}
	return &kubeapi.ContainerStatsResponse{
		Stats: VMStatsToCRIContainerStats(*vs, fsstats.Mountpoint),
	}, nil
}

// ListContainerStats returns stats (same as ContainerStats) for containers
// selected by filter
func (v *VirtletRuntimeService) ListContainerStats(ctx context.Context, in *kubeapi.ListContainerStatsRequest) (*kubeapi.ListContainerStatsResponse, error) {
	filter := CRIContainerStatsFilterToVMStatsFilter(in.GetFilter())
	vmstatsList, err := v.virtTool.ListVMStats(filter)
	if err != nil {
		return nil, err
	}
	fsstats, err := v.virtTool.ImageManager().FilesystemStats()
	if err != nil {
		return nil, err
	}
	var stats []*kubeapi.ContainerStats
	for _, vs := range vmstatsList {
		stats = append(stats, VMStatsToCRIContainerStats(vs, fsstats.Mountpoint))
	}

	return &kubeapi.ListContainerStatsResponse{
		Stats: stats,
	}, nil
}

// VMStatsToCRIContainerStats converts internal representation of vm/container stats
// to corresponding kubeapi type object
func VMStatsToCRIContainerStats(vs types.VMStats, mountpoint string) *kubeapi.ContainerStats {
	return &kubeapi.ContainerStats{
		Attributes: &kubeapi.ContainerAttributes{
			Id: vs.ContainerID,
			Metadata: &kubeapi.ContainerMetadata{
				Name: vs.ContainerID,
			},
		},
		Cpu: &kubeapi.CpuUsage{
			Timestamp:            vs.Timestamp,
			UsageCoreNanoSeconds: &kubeapi.UInt64Value{Value: vs.CpuUsage},
		},
		Memory: &kubeapi.MemoryUsage{
			Timestamp:       vs.Timestamp,
			WorkingSetBytes: &kubeapi.UInt64Value{Value: vs.MemoryUsage},
		},
		WritableLayer: &kubeapi.FilesystemUsage{
			Timestamp: vs.Timestamp,
			FsId: &kubeapi.FilesystemIdentifier{
				Mountpoint: mountpoint,
			},
			UsedBytes:  &kubeapi.UInt64Value{Value: vs.FsBytes},
			InodesUsed: &kubeapi.UInt64Value{Value: 1},
		},
	}
}

// ReopenContainerLog is a placeholder for an unimplemented CRI method.
func (v *VirtletRuntimeService) ReopenContainerLog(ctx context.Context, in *kubeapi.ReopenContainerLogRequest) (*kubeapi.ReopenContainerLogResponse, error) {
	return &kubeapi.ReopenContainerLogResponse{}, nil
}

func validatePodSandboxConfig(config *kubeapi.PodSandboxConfig) error {
	if config.GetMetadata() == nil {
		return errors.New("sandbox config is missing Metadata attribute")
	}

	return nil
}

// RebootVM method implements RebootVM() from CRI
func (v *VirtletRuntimeService) RebootVM(ctx context.Context, in *kubeapi.RebootVMRequest) (*kubeapi.RebootVMResponse, error) {
	glog.V(2).Infof("Rebooting VM %s", in.VmId)
	if err := v.virtTool.RebootVM(in.VmId); err != nil {
		glog.Errorf("RebootVM failed with error: %v", err)
		return nil, err
	}
	glog.V(2).Infof("Successfully Rebooted VM %s", in.VmId)
	response := &kubeapi.RebootVMResponse{}
	return response, nil
}

// To be implemented
func (v *VirtletRuntimeService) AttachNetworkInterface(ctx context.Context, in *kubeapi.DeviceAttachDetachRequest) (*kubeapi.DeviceAttachDetachResponse, error) {
	return nil, errors.New("not implemented")
}

func (v *VirtletRuntimeService) DetachNetworkInterface(ctx context.Context, in *kubeapi.DeviceAttachDetachRequest) (*kubeapi.DeviceAttachDetachResponse, error) {
	return nil, errors.New("not implemented")
}

func (v *VirtletRuntimeService) ListNetworkInterfaces(ctx context.Context, in *kubeapi.ListDeviceRequest) (*kubeapi.ListDeviceResponse, error) {
	return nil, errors.New("not implemented")
}

// CreateSnapshot method implements CreateSnapshot() from CRI
func (v *VirtletRuntimeService) CreateSnapshot(ctx context.Context, in *kubeapi.CreateSnapshotRequest) (*kubeapi.CreateSnapshotResponse, error) {
	glog.V(2).Infof("Creating a snapshot for VM %s", in.VmID)

	err := checkSnapshotName(in.SnapshotID)
	if err != nil {
		return nil, err
	}

	// this flag is for future extension. So far we don't support any flags
	if in.Flags != 0 {
		glog.Warningf("CreateSnapshot: Current runtime server doesn't support flag %v. Ignored.", in.Flags)
	}

	if err := v.virtTool.CreateSnapshot(in.VmID, in.SnapshotID); err != nil {
		glog.Errorf("CreateSnapshot failed for VM %s with error: %v", in.VmID, err)
		return nil, err
	}
	glog.V(2).Infof("Create snapshot %s succeeded for VM %s", in.SnapshotID, in.VmID)

	return &kubeapi.CreateSnapshotResponse{}, nil
}

// RestoreToSnapshot method implements RestoreToSnapshot() from CRI
func (v *VirtletRuntimeService) RestoreToSnapshot(ctx context.Context, in *kubeapi.RestoreToSnapshotRequest) (*kubeapi.RestoreToSnapshotResponse, error) {
	glog.V(2).Infof("RestoreToSnapshot: Restoring VM %s to snapshot %s", in.SnapshotID, in.VmID)

	err := checkSnapshotName(in.SnapshotID)
	if err != nil {
		return nil, err
	}

	// this flag is for future extension. So far we don't support any flags
	if in.Flags != 0 {
		glog.Warningf("RestoreToSnapshot: Current runtime server doesn't support flag %v. Ignored.", in.Flags)
	}

	if err := v.virtTool.RestoreToSnapshot(in.VmID, in.SnapshotID); err != nil {
		glog.Errorf("RestoreToSnapshot: failed for VM %s with error: %v", in.VmID, err)
		return nil, err
	}
	glog.V(2).Infof("RestoreToSnapshot: restore VM %s to snapshot %s successfully", in.VmID, in.SnapshotID)

	return &kubeapi.RestoreToSnapshotResponse{}, nil
}

func checkSnapshotName(snapshotID string) error {

	// some characters will be used internally
	if snapshotID == "" || strings.ContainsAny(snapshotID, "\";<>/") == true {
		return fmt.Errorf("invalid snapshot ID")
	}

	return nil
}
