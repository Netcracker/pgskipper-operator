// Copyright 2024-2025 NetCracker Technology Corporation
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package deployment

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

func TestPostgresTmpVolumeIsMemoryBackedAtFivePercent(t *testing.T) {
	resources := &corev1.ResourceRequirements{
		Limits: corev1.ResourceList{
			corev1.ResourceMemory: resource.MustParse("10Gi"),
		},
	}

	vol := postgresTmpVolume(resources)
	if vol.Name != "tmp" {
		t.Fatalf("volume name = %q, want tmp", vol.Name)
	}
	if vol.EmptyDir == nil {
		t.Fatal("expected an emptyDir volume")
	}
	if vol.EmptyDir.Medium != corev1.StorageMediumMemory {
		t.Fatalf("medium = %q, want Memory", vol.EmptyDir.Medium)
	}
	want := resource.MustParse("512Mi")
	if vol.EmptyDir.SizeLimit == nil || vol.EmptyDir.SizeLimit.Cmp(want) != 0 {
		t.Fatalf("sizeLimit = %v, want %s", vol.EmptyDir.SizeLimit, want.String())
	}
}

func TestPostgresTmpVolumeKeeps100MiWhenFivePercentIsSmaller(t *testing.T) {
	resources := &corev1.ResourceRequirements{
		Limits: corev1.ResourceList{
			corev1.ResourceMemory: resource.MustParse("1000Mi"),
		},
	}

	vol := postgresTmpVolume(resources)
	want := resource.MustParse("100Mi")
	if vol.EmptyDir == nil || vol.EmptyDir.SizeLimit == nil || vol.EmptyDir.SizeLimit.Cmp(want) != 0 {
		t.Fatalf("sizeLimit = %v, want %s", vol.EmptyDir, want.String())
	}
}

func TestPostgresTmpVolumeFallsBackWithoutMemoryLimit(t *testing.T) {
	want := resource.MustParse("100Mi")
	for _, resources := range []*corev1.ResourceRequirements{
		nil,
		{},
		{Limits: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("100m")}},
	} {
		vol := postgresTmpVolume(resources)
		if vol.EmptyDir == nil || vol.EmptyDir.Medium != corev1.StorageMediumMemory {
			t.Fatalf("medium = %v, want Memory", vol.EmptyDir)
		}
		if vol.EmptyDir.SizeLimit == nil || vol.EmptyDir.SizeLimit.Cmp(want) != 0 {
			t.Fatalf("sizeLimit = %v, want %s", vol.EmptyDir.SizeLimit, want.String())
		}
	}
}
