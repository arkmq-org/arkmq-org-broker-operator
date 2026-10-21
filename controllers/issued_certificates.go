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

package controllers

import (
	"context"
	"errors"

	broker "github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v1beta2"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/common"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/pki"
)

// operatorCertRequest is the operator's own client certificate. Shared by every
// CR, so owned by none and never released.
func operatorCertRequest() (pki.Request, error) {
	namespace, err := common.GetOperatorNamespaceFromEnv()
	return pki.Request{Name: common.GetOperatorCertSecretName(), Namespace: namespace}, err
}

// ensureCertificates issues requests from the configured ClusterIssuer, and does
// nothing when none is. Every request is made before the first one not issued
// is reported, so that they are issued together.
func (bits *KubeBits) ensureCertificates(requests ...pki.Request) error {
	issuer := common.GetClusterIssuerName()
	if issuer == "" {
		return nil
	}

	ctx := context.TODO()
	direct, err := pki.Client(bits.Config, bits.Scheme, bits.Client)
	if err != nil {
		return err
	}

	var first error
	for _, request := range requests {
		if err := pki.Ensure(ctx, direct, bits.Scheme, issuer, request); err != nil && first == nil {
			first = err
		}
	}
	return certificateError(first)
}

// certificateError maps what stops issuance to the condition reporting it.
func certificateError(err error) error {
	var conflict *pki.ConflictError
	var notReady *pki.NotReadyError
	switch {
	case errors.As(err, &conflict):
		return NewTransientError(broker.DeployedConditionConflictingCertSecretReason, conflict.Error())
	case errors.As(err, &notReady):
		return NewTransientError(broker.DeployedConditionWaitingForCertificateReason, notReady.Error())
	}
	return err
}
