/*
Copyright 2026.

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

package controllers

import (
	"testing"

	"github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v1beta2"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestAssignNextAvailablePort_FirstPort(t *testing.T) {
	usedPorts := make(map[int32]bool)

	port, err := assignNextAvailablePort(usedPorts)

	assert.NoError(t, err)
	assert.Equal(t, int32(61616), port)
}

func TestAssignNextAvailablePort_MiddlePort(t *testing.T) {
	usedPorts := map[int32]bool{
		61616: true,
		61617: true,
	}

	port, err := assignNextAvailablePort(usedPorts)

	assert.NoError(t, err)
	assert.Equal(t, int32(61618), port)
}

func TestAssignNextAvailablePort_LastPort(t *testing.T) {
	// Mark all ports except the last one as used
	usedPorts := make(map[int32]bool)
	for port := int32(61616); port < 65535; port++ {
		usedPorts[port] = true
	}

	port, err := assignNextAvailablePort(usedPorts)

	assert.NoError(t, err)
	assert.Equal(t, int32(65535), port)
}

func TestAssignNextAvailablePort_Exhausted(t *testing.T) {
	// Mark all ports as used
	usedPorts := make(map[int32]bool)
	for port := int32(61616); port <= 65535; port++ {
		usedPorts[port] = true
	}

	port, err := assignNextAvailablePort(usedPorts)

	assert.Error(t, err)
	assert.Equal(t, int32(0), port)
	assert.Contains(t, err.Error(), "exhausted")
	assert.Contains(t, err.Error(), "61616")
	assert.Contains(t, err.Error(), "65535")
}

// TestCollectUsedPorts_NoApps tests collectUsedPorts with no apps
func TestCollectUsedPorts_NoApps(t *testing.T) {
	apps := []v1beta2.BrokerApp{}

	used := collectUsedPorts(apps, nil)

	assert.Empty(t, used)
}

// TestCollectUsedPorts_SingleApp tests collectUsedPorts with one app
func TestCollectUsedPorts_SingleApp(t *testing.T) {
	apps := []v1beta2.BrokerApp{
		{
			Status: v1beta2.BrokerAppStatus{
				Service: &v1beta2.BrokerServiceBindingStatus{
					AssignedPort: 61616,
				},
			},
		},
	}

	used := collectUsedPorts(apps, nil)

	assert.Len(t, used, 1)
	assert.True(t, used[61616])
}

// TestCollectUsedPorts_MultipleApps tests collectUsedPorts with multiple apps
func TestCollectUsedPorts_MultipleApps(t *testing.T) {
	apps := []v1beta2.BrokerApp{
		{
			Status: v1beta2.BrokerAppStatus{
				Service: &v1beta2.BrokerServiceBindingStatus{
					AssignedPort: 61616,
				},
			},
		},
		{
			Status: v1beta2.BrokerAppStatus{
				Service: &v1beta2.BrokerServiceBindingStatus{
					AssignedPort: 61617,
				},
			},
		},
	}

	used := collectUsedPorts(apps, nil)

	assert.Len(t, used, 2)
	assert.True(t, used[61616])
	assert.True(t, used[61617])
}

// TestCollectUsedPorts_ExcludeApp tests that excludeApp is properly skipped
func TestCollectUsedPorts_ExcludeApp(t *testing.T) {
	excludeApp := &v1beta2.BrokerApp{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "app-to-exclude",
			Namespace: "test",
		},
		Status: v1beta2.BrokerAppStatus{
			Service: &v1beta2.BrokerServiceBindingStatus{
				AssignedPort: 61616,
			},
		},
	}

	apps := []v1beta2.BrokerApp{
		*excludeApp,
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "app-to-include",
				Namespace: "test",
			},
			Status: v1beta2.BrokerAppStatus{
				Service: &v1beta2.BrokerServiceBindingStatus{
					AssignedPort: 61617,
				},
			},
		},
	}

	used := collectUsedPorts(apps, excludeApp)

	// Should only have one port (61617), excluding the app's port (61616)
	assert.Len(t, used, 1)
	assert.False(t, used[61616]) // Excluded app's port
	assert.True(t, used[61617])  // Other app's port
}

// TestCollectUsedPorts_AppsWithNoService tests apps without service binding
func TestCollectUsedPorts_AppsWithNoService(t *testing.T) {
	apps := []v1beta2.BrokerApp{
		{
			Status: v1beta2.BrokerAppStatus{
				Service: nil, // No service binding
			},
		},
		{
			Status: v1beta2.BrokerAppStatus{
				Service: &v1beta2.BrokerServiceBindingStatus{
					AssignedPort: 61616,
				},
			},
		},
	}

	used := collectUsedPorts(apps, nil)

	// Should only count the app with a service binding
	assert.Len(t, used, 1)
	assert.True(t, used[61616])
}

// TestCollectUsedPorts_AppsWithZeroPort tests apps with zero port (not yet assigned)
func TestCollectUsedPorts_AppsWithZeroPort(t *testing.T) {
	apps := []v1beta2.BrokerApp{
		{
			Status: v1beta2.BrokerAppStatus{
				Service: &v1beta2.BrokerServiceBindingStatus{
					AssignedPort: 0, // Not yet assigned
				},
			},
		},
		{
			Status: v1beta2.BrokerAppStatus{
				Service: &v1beta2.BrokerServiceBindingStatus{
					AssignedPort: 61616,
				},
			},
		},
	}

	used := collectUsedPorts(apps, nil)

	// Should only count the app with a real port assignment
	assert.Len(t, used, 1)
	assert.True(t, used[61616])
	assert.False(t, used[0]) // Zero port should not be counted
}
