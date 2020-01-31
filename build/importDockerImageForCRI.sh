#!/bin/bash

WORKING_DIR=/tmp/containerd
CRI_CONFIG_FILE=${WORKING_DIR}/etc/crictl.yaml
CONTAINERD_SOCK_PATH="/run/containerd/containerd.sock"
DOCKER_IMAGE_NAME="arktosstaging/vmruntime:latest"
LOCAL_IMAGE_FILE="localimage.tar"

if ! systemctl is-active --quiet containerd; then
  echo "Containerd is required for exporting docker image for CRI"
  exit 1
fi

echo $CONTAINERD_SOCK_PATH

if [[ ! -e $CONTAINERD_SOCK_PATH ]]; then
  echo "Containerd socket file check failed. Please check containerd socket file path"
  exit 1
fi

mkdir -p ${WORKING_DIR}
cd ${WORKING_DIR}

# Get the ctr and crictl tool from containerd installation package
# Not needed to install the containerd as it is assumed the dev env has it installed via docker.io already
echo "Get containerd package and cri tools"
wget https://storage.googleapis.com/cri-containerd-release/cri-containerd-1.3.0.linux-amd64.tar.gz
tar -xvf cri-containerd-1.3.0.linux-amd64.tar.gz

# Delete existing local image file and save the latest local image
rm -f ${LOCAL_IMAGE_FILE}

echo "Save docker image to local file"
docker save ${DOCKER_IMAGE_NAME} -o ${LOCAL_IMAGE_FILE}

# Import the docker image to the CRI image
${WORKING_DIR}/usr/local/bin/ctr -n k8s.io image import ${LOCAL_IMAGE_FILE} 

# List the image and check
${WORKING_DIR}/usr/local/bin/crictl --config ${CRI_CONFIG_FILE} images 

# Cleanup the tmp folder
rm -f -r ${WORKING_DIR}

