/*
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

	broker "github.com/arkmq-org/arkmq-org-broker-operator/api/v1beta2"
)

func TestAddressTracker_OwnershipDetection(t *testing.T) {
	tracker := newAddressTracker()

	// Local address (owned)
	localAddr := &broker.AddressRef{
		Address: "orders",
		// AppNamespace and AppName empty = owned
	}

	// Cross-app reference (not owned)
	refAddr := &broker.AddressRef{
		Address:      "orders",
		AppNamespace: "other-ns",
		AppName:      "other-app",
	}

	// Track both
	entry1 := tracker.track(localAddr)
	entry2 := tracker.track(refAddr)

	// Should point to the same entry (same address name)
	if len(tracker.names) != 1 {
		t.Errorf("expected 1 tracked address, got %d", len(tracker.names))
	}

	// The entry should be marked as owned (because we tracked the local version)
	if !tracker.names["orders"].isOwned {
		t.Error("expected address to be marked as owned")
	}

	// Both entry pointers should reflect the updated state
	_ = entry1
	_ = entry2
}

func TestAddressTracker_ReferenceOnly(t *testing.T) {
	tracker := newAddressTracker()

	// Only cross-app reference (not owned by this app)
	refAddr := &broker.AddressRef{
		Address:      "shared-queue",
		AppNamespace: "owner-ns",
		AppName:      "owner-app",
	}

	tracker.track(refAddr)

	// Should not be marked as owned
	if tracker.names["shared-queue"].isOwned {
		t.Error("expected address NOT to be marked as owned for cross-app reference")
	}
}

func TestAddressTracker_LocalAddressPrecedence(t *testing.T) {
	tracker := newAddressTracker()

	// Track reference first
	refAddr := &broker.AddressRef{
		Address:      "events",
		AppNamespace: "other-ns",
		AppName:      "other-app",
	}
	tracker.track(refAddr)

	// Then track local ownership
	localAddr := &broker.AddressRef{
		Address: "events",
		// Empty AppNamespace/AppName = owned
	}
	tracker.track(localAddr)

	// Should now be marked as owned
	if !tracker.names["events"].isOwned {
		t.Error("expected address to be marked as owned after tracking local version")
	}
}
