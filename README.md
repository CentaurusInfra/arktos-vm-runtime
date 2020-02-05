# ARKTOS-VM-RUNTIME

Arktos-vm-runtime is a runtime service for [Arktos](https://github.com/futurewei-cloud/arktos) cluster to run VM workloads.

Arktos-vm-runtime implements the extended CRI interface defined in Arktos cluster to support VM workloads. It is based 
on and evolved from [Mirantis Virtlet project](https://github.com/Mirantis/virtlet) with  the extension to current CRI 
interface implementation.

## Major features
### Support for VM workload specific operations
Arktos-vm-runtime extends the current CRI interfaces with initial set of methods to support VM operations, as listed below:

	// RebootVM reboots the VM domain and returns error msg if there is any
	RebootVM(ctx context.Context, in *RebootVMRequest, opts ...grpc.CallOption) (*RebootVMResponse, error)
	// CreateSnapshot creates a snapshot of the current VM domain
	CreateSnapshot(ctx context.Context, in *CreateSnapshotRequest, opts ...grpc.CallOption) (*CreateSnapshotResponse, error)
	// RestoreToSnapshot restores the current VM domain to the given snapshot
	RestoreToSnapshot(ctx context.Context, in *RestoreToSnapshotRequest, opts ...grpc.CallOption) (*RestoreToSnapshotResponse, error)
	// AttachNetworkInterface adds new NIC to the POD-VM
	AttachNetworkInterface(ctx context.Context, in *DeviceAttachDetachRequest, opts ...grpc.CallOption) (*DeviceAttachDetachResponse, error)
	// DetachNetworkInterface removes a NIC to the POD-VM
	DetachNetworkInterface(ctx context.Context, in *DeviceAttachDetachRequest, opts ...grpc.CallOption) (*DeviceAttachDetachResponse, error)
	// ListNetworkInterfaces lists NICs attached to the POD-VM
	ListNetworkInterfaces(ctx context.Context, in *ListDeviceRequest, opts ...grpc.CallOption) (*ListDeviceResponse, error)

### VM centric runtime service
Arktos supports VM workload and multiple tenants natively. As a runtime service designed to support Arktos, Arktos-vm-runtime 
bridges the NICs/VPCs from the Arktos node agent to the CNI, with extension to the current CRI's PodSandboxConfig with 
new VPC and NICs fields. Arktos runtime also retrieves information for other VM specific elements such as TTY, CloudInit etc.
from the virtual machine workload definition to the underlying Libvirt component for those VM specific features.


## Build and publish Arktos-vm-runtime images
Arktos-vm-runtime inherits Virtlet's build logic, with addition to publish the runtime docker images with specific tags.
For example, the following commands will build the Arktos-vm-runtime docker image and publish it to docker repo with
tag "0.5.2"

     ./build/cmd.sh build/
     ./build/cmd.sh publish "0.5.2"

## Using Arktos-vm-runtime with Arktos cluster for VM type
Arktos is fully automated to use the Arktos-vm-runtime as default VM runtime service. The Arktos-up.sh in the Arktos 
project can be used to start a onebox Arktos cluster and use Arktos-vm-runtime for VM type workload.

## Work in progress
The Arktos-vm-runtime is still in its early stage. There are quite a few efforts planned in order to make this a complete 
runtime service for VM workloads. 
1. Support More VM actions
2. Simplify the networking design
3. Cleaner Interface definitions and dedicated interfaces for VM workload type


## Licensing

Unless specifically noted, all parts of this project are licensed under the [Apache 2.0 license](LICENSE).

