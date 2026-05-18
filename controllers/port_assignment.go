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
	"fmt"

	broker "github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v1beta2"
)

const (
	// UnassignedPort is the sentinel value indicating no port has been assigned yet
	UnassignedPort = 0

	// DefaultStartPort is the first port to assign.
	// Uses ActiveMQ Artemis default port (61616) as the starting point.
	DefaultStartPort = 61616

	// MaxValidPort is the maximum valid port number (TCP/UDP port range limit)
	MaxValidPort = 65535
)

// assignNextAvailablePort finds the next available port starting from DefaultStartPort
func assignNextAvailablePort(usedPorts map[int32]bool) (int32, error) {
	for port := int32(DefaultStartPort); port <= MaxValidPort; port++ {
		if !usedPorts[port] {
			return port, nil
		}
	}
	return 0, fmt.Errorf("all ports exhausted: range [%d-%d] fully allocated",
		DefaultStartPort, MaxValidPort)
}

// collectUsedPorts gathers ports already assigned on this service
func collectUsedPorts(apps []broker.BrokerApp, excludeApp *broker.BrokerApp) map[int32]bool {
	used := make(map[int32]bool)
	for _, app := range apps {
		// Skip the app we're assigning (if reassigning)
		if excludeApp != nil && app.Namespace == excludeApp.Namespace && app.Name == excludeApp.Name {
			continue
		}
		if app.Status.Service != nil && app.Status.Service.AssignedPort != UnassignedPort {
			used[app.Status.Service.AssignedPort] = true
		}
	}
	return used
}
