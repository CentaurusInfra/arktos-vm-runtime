#!/usr/bin/env bash

# Copyright 2020 Authors of Arktos.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

# Standalone, docker container based vm runtime deployment
# Uses local ENV vriables or local config file and decoupled from k8s
#
# For now, it aligns with the virtlet-ds.yaml and used same set of scripts
# to start the vms, libvirt and virtlet, as well as the node preparation
# 

VMS_CONTAINER_NAME=vmruntime_vms
LIBVIRT_CONTAINER_NAME=vmruntime_libvirt
VIRTLET_CONTAINER_NAME=vmruntime_virtlet

RUNTIME_IMAGE_VERSION=${RUNTIME_IMAGE_VERSION:-"latest"}
KUBE_FLEX_VOLUME_PLUGIN_DIR=${KUBE_FLEX_VOLUME_PLUGIN_DIR:-"/usr/libexec/kubernetes/kubelet-plugins/volume/exec"}
OVERWRITE_DEPLOYMENT_FILES=${OVERWRITE_DEPLOYMENT_FILES:-"false"}
APPARMOR_ENABLED=${APPARMOR_ENABLED:-"false"}
APPARMOR_PROFILE_VIRTLET=virtlet
APPARMOR_PROFILE_LIBVIRTD=libvirtd
APPARMOR_PROFILE_LIBVIRT_QEMU=libvirt-qemu
APPARMOR_PROFILE_VMS=vms

VIRTLET_DEPLOYMENT_FILES_DIR=${VIRTLET_DEPLOYMENT_FILES_DIR:-"/tmp"}
VIRTLET_DEPLOYMENT_FILES_SRC=${VIRTLET_DEPLOYMENT_FILES_SRC:-"https://raw.githubusercontent.com/futurewei-cloud/arktos-vm-runtime/release-0.5/deploy"}

# Add more env as needed or support extra config with the optional command args
VIRTLET_LOGLEVEL=${VIRTLET_LOGLEVEL:-"4"}
VIRTLET_DISABLE_KVM=${VIRTLET_DISABLE_KVM:-"y"}

usage() {
	echo "Invalid usage. Usage: "
	echo "\t$0 start | cleanup [optionl extra args]"
	exit 1
}

cleanup() {
	echo "Stop vm runtime docker containers"
	docker rm -f ${VMS_CONTAINER_NAME}
	docker rm -f ${LIBVIRT_CONTAINER_NAME}
	docker rm -f ${VIRTLET_CONTAINER_NAME}

	echo "Delete vm runtime meta data files"
	rm -f -r /var/lib/virtlet/
	rm -f -r /var/log/virtlet/
	rm -f /var/run/libvirt/libvirt-sock
}

downloadRuntimeDeploymentFiles() {
	# Get runtime deployment files
	echo "Getting runtime deployment files"
	if [[ (${OVERWRITE_DEPLOYMENT_FILES} == "true") || (! -f ${VIRTLET_DEPLOYMENT_FILES_DIR}/${APPARMOR_PROFILE_LIBVIRT_QEMU}) ]]; then
		wget -O ${VIRTLET_DEPLOYMENT_FILES_DIR}/${APPARMOR_PROFILE_LIBVIRT_QEMU}  ${VIRTLET_DEPLOYMENT_FILES_SRC}/apparmor/${APPARMOR_PROFILE_LIBVIRT_QEMU}
	fi

	if [[ (${OVERWRITE_DEPLOYMENT_FILES} == "true") || (! -f ${VIRTLET_DEPLOYMENT_FILES_DIR}/${APPARMOR_PROFILE_LIBVIRTD}) ]]; then
		wget -O ${VIRTLET_DEPLOYMENT_FILES_DIR}/${APPARMOR_PROFILE_LIBVIRTD}  ${VIRTLET_DEPLOYMENT_FILES_SRC}/apparmor/${APPARMOR_PROFILE_LIBVIRTD}
	fi

	if [[ (${OVERWRITE_DEPLOYMENT_FILES} == "true") || (! -f ${VIRTLET_DEPLOYMENT_FILES_DIR}/${APPARMOR_PROFILE_VIRTLET}) ]]; then
		wget -O ${VIRTLET_DEPLOYMENT_FILES_DIR}/${APPARMOR_PROFILE_VIRTLET}  ${VIRTLET_DEPLOYMENT_FILES_SRC}/apparmor/${APPARMOR_PROFILE_VIRTLET}
	fi

	if [[ (${OVERWRITE_DEPLOYMENT_FILES} == "true") || (! -f ${VIRTLET_DEPLOYMENT_FILES_DIR}/${APPARMOR_PROFILE_VMS}) ]]; then
		wget -O ${VIRTLET_DEPLOYMENT_FILES_DIR}/${APPARMOR_PROFILE_VMS}  ${VIRTLET_DEPLOYMENT_FILES_SRC}/apparmor/${APPARMOR_PROFILE_VMS}
	fi
}

enableApparmor() {
	  echo "Config environment under apparmor enabled host"
	  # Start AppArmor service before we have scripts to configure it properly
	  if ! sudo systemctl is-active --quiet apparmor; then
	    echo "Starting Apparmor service"
	    sudo systemctl start apparmor
	  fi

	  # install runtime apparmor profiles and reload apparmor
	  echo "Installing arktos runtime apparmor profiles"
	  cp ${VIRTLET_DEPLOYMENT_FILES_DIR}/${APPARMOR_PROFILE_LIBVIRT_QEMU} /etc/apparmor.d/abstractions/
	  sudo install -m 0644 ${VIRTLET_DEPLOYMENT_FILES_DIR}/${APPARMOR_PROFILE_LIBVIRTD} -t /etc/apparmor.d/
	  sudo install -m 0644 ${VIRTLET_DEPLOYMENT_FILES_DIR}/${APPARMOR_PROFILE_VIRTLET} -t /etc/apparmor.d/ 
	  sudo install -m 0644 ${VIRTLET_DEPLOYMENT_FILES_DIR}/${APPARMOR_PROFILE_VMS} -t /etc/apparmor.d/
	  sudo apparmor_parser -r /etc/apparmor.d/${APPARMOR_PROFILE_LIBVIRTD}
	  sudo apparmor_parser -r /etc/apparmor.d/${APPARMOR_PROFILE_VIRTLET}
	  sudo apparmor_parser -r /etc/apparmor.d/${APPARMOR_PROFILE_VMS}
	  echo "Completed"
}

startRuntime() {
	if [[ "${APPARMOR_ENABLED}" == "true" ]]; then
		downloadRuntimeDeploymentFiles
		enableApparmor
	fi

	echo "Start vm runtime containers"
	#virtlet container bind host directories
	mkdir -p /var/lib/virtlet/vms
	mkdir -p /var/log/virtlet/vms
	mkdir -p /var/run/libvirt
	mkdir -p /var/run/netns
	mkdir -p /var/lib/virtlet/volumes

	if [ ! -f "${KUBE_FLEX_VOLUME_PLUGIN_DIR}" ]; then
                mkdir -p "${KUBE_FLEX_VOLUME_PLUGIN_DIR}"
	fi

	DOCKER_RUN_CMD="docker run"

	${DOCKER_RUN_CMD} --net=host --privileged --pid=host --uts=host --ipc=host --user=root \
	--env VIRTLET_LOGLEVEL=${VIRTLET_LOGLEVEL} \
	--env VIRTLET_DISABLE_KVM=${VIRTLET_DISABLE_KVM} \
	--mount type=bind,src=/dev,dst=/dev \
	--mount type=bind,src=/var/lib,dst=/host-var-lib \
	--mount type=bind,src=/run,dst=/run \
	--mount type=bind,src=${KUBE_FLEX_VOLUME_PLUGIN_DIR},dst=/kubelet-volume-plugins \
	--mount type=bind,src=/var/lib/virtlet,dst=/var/lib/virtlet,bind-propagation=rshared \
	--mount type=bind,src=/var/log,dst=/hostlog \
	arktosstaging/vmruntime:${RUNTIME_IMAGE_VERSION} /bin/bash -c "/prepare-node.sh > /hostlog/virtlet/prepare-node.log 2>&1 "

	echo $(date -u +%Y-%m-%dT%H:%M:%SZ) "START VMS LOG FILE" > /var/log/virtlet/vms.log
	DOCKER_RUN_CMD="docker run"
	if [[ "${APPARMOR_ENABLED}" == "true" ]]; then
                DOCKER_RUN_CMD="docker run --security-opt apparmor=${APPARMOR_PROFILE_VMS}"
	fi

	${DOCKER_RUN_CMD} --net=host --privileged --pid=host --uts=host --ipc=host --user=root \
	--name ${VMS_CONTAINER_NAME} \
	--mount type=bind,src=/dev,dst=/dev \
	--mount type=bind,src=/lib/modules,dst=/lib/modules,readonly \
	--mount type=bind,src=/var/lib/libvirt,dst=/var/lib/libvirt \
	--mount type=bind,src=/var/lib/virtlet,dst=/var/lib/virtlet,bind-propagation=rshared \
	--mount type=bind,src=/var/log/virtlet,dst=/var/log/virtlet \
	--mount type=bind,src=/var/log/virtlet/vms,dst=/var/log/vms \
	arktosstaging/vmruntime:${RUNTIME_IMAGE_VERSION} /bin/bash -c "/vms.sh >> /var/log/virtlet/vms.log 2>&1 " &

	echo $(date -u +%Y-%m-%dT%H:%M:%SZ) "START LIBVIRT LOG FILE" > /var/log/virtlet/libvirt.log
	DOCKER_RUN_CMD="docker run"
        if [[ "${APPARMOR_ENABLED}" == "true" ]]; then
                DOCKER_RUN_CMD="docker run --security-opt apparmor=${APPARMOR_PROFILE_LIBVIRTD}"
        fi
	${DOCKER_RUN_CMD} --net=host --privileged --pid=host --uts=host --ipc=host --user=root \
	--name ${LIBVIRT_CONTAINER_NAME} \
	--mount type=bind,src=/boot,dst=/boot,readonly \
	--mount type=bind,src=/dev,dst=/dev \
	--mount type=bind,src=/var/lib,dst=/var/lib \
	--mount type=bind,src=/etc/libvirt/qemu,dst=/etc/libvirt/qemu \
	--mount type=bind,src=/lib/modules,dst=/lib/modules,readonly \
	--mount type=bind,src=/run,dst=/run \
	--mount type=bind,src=/sys/fs/cgroup,dst=/sys/fs/cgroup \
	--mount type=bind,src=/var/lib/libvirt,dst=/var/lib/libvirt \
	--mount type=bind,src=/var/lib/virtlet,dst=/var/lib/virtlet,bind-propagation=rshared \
	--mount type=bind,src=/var/log/virtlet,dst=/var/log/virtlet \
	--mount type=bind,src=/var/log/libvirt,dst=/var/log/libvirt \
	--mount type=bind,src=/var/log/virtlet/vms,dst=/var/log/vms \
	--mount type=bind,src=/var/run/libvirt,dst=/var/run/libvirt \
	arktosstaging/vmruntime:${RUNTIME_IMAGE_VERSION} /bin/bash -c "/libvirt.sh >> /var/log/virtlet/libvirt.log 2>&1" &

	echo $(date -u +%Y-%m-%dT%H:%M:%SZ) "START VIRTLET LOG FILE" > /var/log/virtlet/virtlet.log
	DOCKER_RUN_CMD="docker run"
        if [[ "${APPARMOR_ENABLED}" == "true" ]]; then
                DOCKER_RUN_CMD="docker run --security-opt apparmor=${APPARMOR_PROFILE_VIRTLET}"
        fi      
	${DOCKER_RUN_CMD} --net=host --privileged --pid=host --uts=host --ipc=host --user=root \
	--name ${VIRTLET_CONTAINER_NAME} \
	--env VIRTLET_LOGLEVEL=${VIRTLET_LOGLEVEL} \
        --env VIRTLET_DISABLE_KVM=${VIRTLET_DISABLE_KVM} \
	--mount type=bind,src=/etc/cni/net.d,dst=/etc/cni/net.d \
	--mount type=bind,src=/opt/cni/bin,dst=/opt/cni/bin \
	--mount type=bind,src=/boot,dst=/boot,readonly \
	--mount type=bind,src=/dev,dst=/dev \
	--mount type=bind,src=/var/lib,dst=/var/lib \
	--mount type=bind,src=/etc/libvirt/qemu,dst=/etc/libvirt/qemu \
	--mount type=bind,src=/lib/modules,dst=/lib/modules,readonly \
	--mount type=bind,src=/run,dst=/run \
	--mount type=bind,src=/sys/fs/cgroup,dst=/sys/fs/cgroup \
	--mount type=bind,src=${KUBE_FLEX_VOLUME_PLUGIN_DIR},dst=/kubelet-volume-plugins \
	--mount type=bind,src=/var/lib/libvirt,dst=/var/lib/libvirt \
	--mount type=bind,src=/var/lib/virtlet,dst=/var/lib/virtlet,bind-propagation=rshared \
	--mount type=bind,src=/var/log,dst=/var/log \
	--mount type=bind,src=/var/log/virtlet,dst=/var/log/virtlet \
	--mount type=bind,src=/var/log/virtlet/vms,dst=/var/log/vms \
	--mount type=bind,src=/var/run/libvirt,dst=/var/run/libvirt \
	--mount type=bind,src=/var/run/netns,dst=/var/run/netns,bind-propagation=rshared \
	arktosstaging/vmruntime:${RUNTIME_IMAGE_VERSION} /bin/bash -c "/start.sh >> /var/log/virtlet/virtlet.log 2>&1" &
}

op=$1

# Should there be more OPs, change to switch
if [ "$op" = "start" ]; then
        shift
	startRuntime $*
	exit 0
fi

if [ "$op" = "cleanup" ]; then
        shift
        cleanup $*
        exit 0
fi

# Print usage for not supported operations
usage

exit 1

