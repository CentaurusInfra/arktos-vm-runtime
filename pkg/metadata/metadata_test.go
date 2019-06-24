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

package metadata

import (
	"testing"

	"github.com/boltdb/bolt"
	"github.com/jonboulle/clockwork"

	"github.com/Mirantis/virtlet/pkg/metadata/fake"
	"github.com/Mirantis/virtlet/pkg/metadata/types"
)

func dumpDB(t *testing.T, store Store, context string) error {
	db := store.(*boltClient).db
	t.Logf("==[ %s ]==> Start DB dump", context)
	err := db.View(func(tx *bolt.Tx) error {
		var iterateOverElements func(tx *bolt.Tx, bucket *bolt.Bucket, indent string)
		iterateOverElements = func(tx *bolt.Tx, bucket *bolt.Bucket, indent string) {
			var c *bolt.Cursor
			if bucket == nil {
				c = tx.Cursor()
			} else {
				c = bucket.Cursor()
			}
			for k, v := c.First(); k != nil; k, v = c.Next() {
				if v == nil {
					// SubBucket
					t.Logf(" %s BUCKET: %s", indent, string(k))
					if bucket == nil {
						// root bucket
						iterateOverElements(tx, tx.Bucket(k), "  "+indent)
					} else {
						iterateOverElements(tx, bucket.Bucket(k), "  "+indent)
					}
				} else {
					t.Logf(" %s %s: %s\n", indent, string(k), string(v))
				}
			}
		}
		iterateOverElements(tx, nil, "|_")
		return nil
	})
	t.Logf("==[ %s ]==> End DB dump", context)

	return err
}

func setUpTestStore(t *testing.T, sandboxConfigs []*types.PodSandboxConfig, containerConfigs []*fake.ContainerTestConfig, clock clockwork.Clock) Store {
	store, err := NewFakeStore()
	if err != nil {
		t.Fatal(err)
	}
	// Check filter on empty DB
	sandboxList, err := store.ListPodSandboxes(&types.PodSandboxFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(sandboxList) != 0 {
		t.Errorf("ListPodSandboxes() returned non-empty result for an empty db")
	}

	if clock == nil {
		clock = clockwork.NewRealClock()
	}
	for _, sandbox := range sandboxConfigs {
		psi, _ := NewPodSandboxInfo(sandbox, "", types.PodSandboxState_SANDBOX_READY, clock)
		if err := store.PodSandbox(sandbox.Uid).Save(
			func(c *types.PodSandboxInfo) (*types.PodSandboxInfo, error) {
				return psi, nil
			},
		); err != nil {
			t.Fatal(err)
		}
	}

	for _, container := range containerConfigs {
		ci := &types.ContainerInfo{
			Name:      container.Name,
			CreatedAt: clock.Now().UnixNano(),
			Config: types.VMConfig{
				PodSandboxID:         container.SandboxID,
				Image:                container.Image,
				ContainerLabels:      container.Labels,
				ContainerAnnotations: container.Annotations,
			},
		}
		if err := store.Container(container.ContainerID).Save(
			func(c *types.ContainerInfo) (*types.ContainerInfo, error) {
				return ci, nil
			},
		); err != nil {
			t.Fatal(err)
		}
	}

	dumpDB(t, store, "init")

	return store
}
