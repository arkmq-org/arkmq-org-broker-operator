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

	broker "github.com/arkmq-org/arkmq-org-broker-operator/api/v1beta2"
)

const (
	// UnassignedPort is the sentinel value indicating no port has been assigned yet
	UnassignedPort = 0

	// DefaultStartPort is the first port in the default pool when no NetworkPolicy is present.
	// Uses ActiveMQ Artemis default port (61616) as the starting point.
	DefaultStartPort = 61616

	// MaxPortOffset defines the size of the default port pool (ports 61616-62615).
	// This limits the maximum number of BrokerApp instances per BrokerService to 1000
	// in the absence of NetworkPolicy constraints. This prevents unbounded port allocation
	// while supporting large deployments. Services with more than 1000 apps should use
	// NetworkPolicy-based port pools or deploy multiple BrokerService instances.
	MaxPortOffset = 1000
)

// assignPortFromPool assigns next available port from discovered pool
// Precondition: poolInfo must not be nil (caller should check before calling)
func assignPortFromPool(poolInfo *broker.PortPoolInfo, usedPorts map[int32]bool) (int32, error) {
	// Case 1: Explicit port list (from NetworkPolicy)
	if poolInfo.Ports != nil {
		for _, port := range poolInfo.Ports {
			if !usedPorts[port] {
				return port, nil
			}
		}
		return 0, fmt.Errorf("port pool exhausted: all %d ports in use", len(poolInfo.Ports))
	}

	// Case 2: Port range (default pool)
	if poolInfo.PortRange != nil {
		for port := poolInfo.PortRange.Start; port <= poolInfo.PortRange.End; port++ {
			if !usedPorts[port] {
				return port, nil
			}
		}
		return 0, fmt.Errorf("port pool exhausted: range [%d-%d] fully allocated",
			poolInfo.PortRange.Start, poolInfo.PortRange.End)
	}

	return 0, fmt.Errorf("invalid port pool info: neither Ports nor PortRange specified")
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
