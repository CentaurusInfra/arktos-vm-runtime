/*
Copyright 2017 Mirantis

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
	"fmt"
	"reflect"
	"testing"

	"github.com/Mirantis/virtlet/pkg/metadata/fake"
	"github.com/Mirantis/virtlet/pkg/metadata/types"
)

func TestSetGetContainerInfo(t *testing.T) {
	sandboxes := fake.GetSandboxes(2)
	containers := fake.GetContainersConfig(sandboxes)

	store := setUpTestStore(t, sandboxes, containers, nil)

	for _, container := range containers {
		containerInfo, err := store.Container(container.ContainerID).Retrieve()
		if err != nil {
			t.Fatal(err)
		}
		if containerInfo == nil {
			t.Fatal(fmt.Errorf("containerInfo of container %q not found in Virtlet metadata store", container.ContainerID))
		}

		if containerInfo.Id != container.ContainerID {
			t.Errorf("Expected %s, instead got %s", container.ContainerID, containerInfo.Id)
		}

		if containerInfo.Config.PodSandboxID != container.SandboxID {
			t.Errorf("Expected %s, instead got %s", container.SandboxID, containerInfo.Config.PodSandboxID)
		}

		if containerInfo.Config.Image != container.Image {
			t.Errorf("Expected %s, instead got %s", container.Image, containerInfo.Config.Image)
		}

		if !reflect.DeepEqual(containerInfo.Config.ContainerLabels, container.Labels) {
			t.Errorf("Expected %v, instead got %v", container.Labels, containerInfo.Config.ContainerLabels)
		}

		if !reflect.DeepEqual(containerInfo.Config.ContainerAnnotations, container.Annotations) {
			t.Errorf("Expected %v, instead got %v", container.Annotations, containerInfo.Config.ContainerAnnotations)
		}
	}
}

func TestGetImagesInUse(t *testing.T) {
	sandboxes := fake.GetSandboxes(2)
	containers := fake.GetContainersConfig(sandboxes)

	store := setUpTestStore(t, sandboxes, containers, nil)

	expectedImagesInUse := map[string]bool{"testImage": true}
	imagesInUse, err := store.ImagesInUse()
	if err != nil {
		t.Fatalf("ImagesInUse(): %v", err)
	}
	if !reflect.DeepEqual(imagesInUse, expectedImagesInUse) {
		t.Errorf("bad result from ImagesInUse(): expected %#v, got #%v", expectedImagesInUse, imagesInUse)
	}
}

func TestRemoveContainer(t *testing.T) {
	sandboxes := fake.GetSandboxes(2)
	containers := fake.GetContainersConfig(sandboxes)

	store := setUpTestStore(t, sandboxes, containers, nil)

	for _, container := range containers {
		podContainers, err := store.ListPodContainers(container.SandboxID)
		if err != nil {
			t.Fatal(err)
		}
		if len(podContainers) != 1 || podContainers[0].GetID() != container.ContainerID {
			t.Errorf("Unexpected container list length: %d != 1", len(podContainers))
		}
		if err := store.Container(container.ContainerID).Save(func(c *types.ContainerInfo) (*types.ContainerInfo, error) {
			return nil, nil
		}); err != nil {
			t.Fatal(err)
		}
		podContainers, err = store.ListPodContainers(container.SandboxID)
		if err != nil {
			t.Fatal(err)
		}
		if len(podContainers) != 0 {
			t.Errorf("Unexpected container list length: %d != 0", len(podContainers))
		}
	}
}
