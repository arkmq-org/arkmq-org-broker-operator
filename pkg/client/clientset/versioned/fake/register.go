/*
Copyright 2020 The Kubernetes Authors.

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

package fake

import (
	brokerv1beta1 "github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v1beta1"
	brokerv2alpha1 "github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v2alpha1"
	brokerv2alpha2 "github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v2alpha2"
	brokerv2alpha3 "github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v2alpha3"
	brokerv2alpha4 "github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v2alpha4"
	brokerv2alpha5 "github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v2alpha5"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	runtime "k8s.io/apimachinery/pkg/runtime"
	schema "k8s.io/apimachinery/pkg/runtime/schema"
	serializer "k8s.io/apimachinery/pkg/runtime/serializer"
)

var scheme = runtime.NewScheme()
var codecs = serializer.NewCodecFactory(scheme)
var parameterCodec = runtime.NewParameterCodec(scheme)

func init() {
	v1.AddToGroupVersion(scheme, schema.GroupVersion{Version: "v1"})
	AddToScheme(scheme)
}

// AddToScheme adds all types of this clientset into the given scheme. This allows composition
// of clientsets, like in:
//
//	import (
//	  "k8s.io/client-go/kubernetes"
//	  clientsetscheme "k8s.io/client-go/kuberentes/scheme"
//	  aggregatorclientsetscheme "k8s.io/kube-aggregator/pkg/client/clientset_generated/clientset/scheme"
//	)
//
//	kclientset, _ := kubernetes.NewForConfig(c)
//	aggregatorclientsetscheme.AddToScheme(clientsetscheme.Scheme)
//
// After this, RawExtensions in Kubernetes types will serialize kube-aggregator types
// correctly.
func AddToScheme(scheme *runtime.Scheme) {
	brokerv2alpha1.SchemeBuilder.AddToScheme(scheme)
	brokerv2alpha2.SchemeBuilder.AddToScheme(scheme)
	brokerv2alpha3.SchemeBuilder.AddToScheme(scheme)
	brokerv2alpha4.SchemeBuilder.AddToScheme(scheme)
	brokerv2alpha5.SchemeBuilder.AddToScheme(scheme)
	brokerv1beta1.SchemeBuilder.AddToScheme(scheme)
}
