/*
Copyright 2021.

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

package pki

import (
	"sync"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	directLock    sync.Mutex
	directClients = map[*rest.Config]client.Client{}
)

// Client returns a client that reads from the API server rather than from the
// manager's cache. Reading Certificates through the cache would start an
// informer over every Certificate the operator can see, needing list and watch
// on a kind that is only read on reconcile. Without a config, as in unit tests,
// it returns cached.
func Client(config *rest.Config, scheme *runtime.Scheme, cached client.Client) (client.Client, error) {
	if config == nil {
		return cached, nil
	}

	directLock.Lock()
	defer directLock.Unlock()

	if direct, found := directClients[config]; found {
		return direct, nil
	}
	direct, err := client.New(config, client.Options{Scheme: scheme})
	if err != nil {
		return nil, err
	}
	directClients[config] = direct
	return direct, nil
}
