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
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"

	broker "github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v1beta2"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/jolokia_client"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// The drain state feeds the admission policy that refuses changes which would
// drop a queue holding messages. The operator never drains: users revoke
// producerOf, let consumers empty the queues, then remove them.

type queueCounter func(address, queue, routingType string) (int64, error)

// newQueueCounter is a variable so unit tests can stand in for the broker
var newQueueCounter = func(cl client.Client, service *broker.BrokerService) (queueCounter, error) {
	brokerCR := &broker.Broker{}
	if err := cl.Get(context.TODO(), types.NamespacedName{Name: service.Name, Namespace: service.Namespace}, brokerCR); err != nil {
		return nil, err
	}
	agents := jolokia_client.GetMinimalJolokiaAgentsForBroker(brokerCR, cl)
	if len(agents) == 0 {
		return nil, fmt.Errorf("no jolokia agent for broker %s/%s", brokerCR.Namespace, brokerCR.Name)
	}
	return agents[0].Artemis.GetQueueMessageCount, nil
}

type drainQueue struct {
	address     string
	name        string
	routingType string
}

// processDrainState publishes the message counts of the queues this app
// declares, read against the generation the bound service provisioned. It
// reports whether the counts should be read again soon.
func (reconciler *BrokerAppInstanceReconciler) processDrainState() (refresh bool) {
	if reconciler.status.Service == nil || reconciler.service == nil {
		reconciler.status.Addresses = nil
		reconciler.status.AddressesObservedGeneration = reconciler.instance.Generation
		return false
	}

	if !reconciler.isProvisioned(reconciler.instance) {
		// the broker may still run a previous configuration, keep the last
		// observation, its generation tells the policy it is stale
		return false
	}

	addresses, queues, owners := drainStateFromSpec(reconciler.instance)

	granted := grantsSend(reconciler.instance)
	for address, owner := range owners {
		granted[address] = reconciler.ownerGrantsSend(owner, address)
	}
	for address, e := range addresses {
		e.SendGranted = granted[address]
	}

	var count queueCounter
	var counterErr error
	for _, q := range queues {
		if !granted[q.address] {
			count, counterErr = newQueueCounter(reconciler.Client, reconciler.service)
			break
		}
	}
	if counterErr != nil {
		reconciler.log.V(1).Info("unable to reach broker for queue counts", "error", counterErr)
	}

	for _, q := range queues {
		// a queue that may still receive messages cannot settle, its count is not read
		messages := broker.QueueDrainUnknown
		if !granted[q.address] {
			if count != nil {
				if n, err := count(q.address, q.name, q.routingType); err == nil {
					messages = n
				} else {
					reconciler.log.V(1).Info("unable to read queue count", "address", q.address, "queue", q.name, "error", err)
				}
			}
			if messages != 0 {
				// draining, or not readable yet
				refresh = true
			}
		}
		entry := addresses[q.address]
		entry.Queues = append(entry.Queues, broker.QueueDrainStatus{Name: q.name, MessageCount: messages})
	}

	reconciler.status.Addresses = sortedDrainStatus(addresses)
	reconciler.status.AddressesObservedGeneration = reconciler.instance.Generation
	return refresh
}

func (reconciler *BrokerAppInstanceReconciler) isProvisioned(app *broker.BrokerApp) bool {
	provisioned := meta.FindStatusCondition(reconciler.service.Status.Conditions, broker.AppsProvisionedConditionType)
	return provisioned != nil && provisioned.Status == metav1.ConditionTrue &&
		slices.Contains(reconciler.service.Status.ProvisionedApps, AppIdentityWithGeneration(app))
}

// ownerGrantsSend reads send permission from the app owning an address this
// app consumes; when the owner cannot be read the grant is assumed
func (reconciler *BrokerAppInstanceReconciler) ownerGrantsSend(owner types.NamespacedName, address string) bool {
	app := &broker.BrokerApp{}
	if err := reconciler.Client.Get(context.TODO(), owner, app); err != nil {
		reconciler.log.V(1).Info("unable to read address owner", "owner", owner, "error", err)
		return true
	}
	return grantsSend(app)[address]
}

// drainStateFromSpec lists the queues the app's configuration declares on the
// broker, mirroring processCapabilities, and the owner of each address it
// consumes from another app. Anycast references to another app's address are
// left to that owner: its configuration keeps the queue.
func drainStateFromSpec(app *broker.BrokerApp) (map[string]*broker.AddressDrainStatus, []drainQueue, map[string]types.NamespacedName) {
	addresses := map[string]*broker.AddressDrainStatus{}
	var queues []drainQueue
	owners := map[string]types.NamespacedName{}
	seen := map[string]bool{}
	ownerOf := func(ref broker.AddressRef) {
		if ref.AppName == "" {
			return
		}
		ns := ref.AppNamespace
		if ns == "" {
			ns = app.Namespace
		}
		owners[extractBaseAddress(ref.Address)] = types.NamespacedName{Namespace: ns, Name: ref.AppName}
	}

	entry := func(address string) *broker.AddressDrainStatus {
		e, ok := addresses[address]
		if !ok {
			e = &broker.AddressDrainStatus{Address: address}
			addresses[address] = e
		}
		return e
	}
	addQueue := func(address, name, routingType string) {
		key := address + FQQNSeparator + name
		if seen[key] {
			return
		}
		seen[key] = true
		entry(address)
		queues = append(queues, drainQueue{address: address, name: name, routingType: routingType})
	}

	for _, addr := range app.Spec.Addresses {
		if isMulticastAddress(addr.PubSub, addr.Subscriptions) {
			for _, sub := range addr.Subscriptions {
				addQueue(addr.Address, sub, "multicast")
			}
		} else {
			addQueue(addr.Address, addr.Address, "anycast")
		}
	}

	for _, capability := range app.Spec.Capabilities {
		for _, ref := range append(append([]broker.AddressRef{}, capability.ProducerOf...), capability.ConsumerOf...) {
			if fqqn := strings.SplitN(ref.Address, FQQNSeparator, 2); len(fqqn) == 2 {
				addQueue(fqqn[0], fqqn[1], "anycast")
				ownerOf(ref)
			}
		}
		for _, ref := range capability.ConsumerOf {
			if isMulticastAddress(ref.PubSub, ref.Subscriptions) {
				for _, sub := range ref.Subscriptions {
					addQueue(extractBaseAddress(ref.Address), sub, "multicast")
				}
				ownerOf(ref)
			}
		}
	}

	return addresses, queues, owners
}

// grantsSend lists the addresses this app owns and still permits sending to,
// through its own producerOf or a producerOfAppSelector for other apps
func grantsSend(app *broker.BrokerApp) map[string]bool {
	granted := map[string]bool{}
	for _, addr := range app.Spec.Addresses {
		if addr.ProducerOfAppSelector != "" {
			granted[addr.Address] = true
		}
	}
	for _, capability := range app.Spec.Capabilities {
		for _, ref := range capability.ProducerOf {
			if ref.AppName == "" {
				granted[extractBaseAddress(ref.Address)] = true
			}
		}
	}
	return granted
}

func sortedDrainStatus(addresses map[string]*broker.AddressDrainStatus) []broker.AddressDrainStatus {
	if len(addresses) == 0 {
		return nil
	}
	result := make([]broker.AddressDrainStatus, 0, len(addresses))
	for _, e := range addresses {
		sort.Slice(e.Queues, func(i, j int) bool { return e.Queues[i].Name < e.Queues[j].Name })
		result = append(result, *e)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Address < result[j].Address })
	return result
}
