package chainoftrust

import (
	"crypto/x509/pkix"
	"fmt"

	v1beta2 "github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v1beta2"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/common"
	rtclient "sigs.k8s.io/controller-runtime/pkg/client"
)

// CertIdentities holds the resolved certificate subjects and secret references
// needed to configure the broker's control-plane mTLS (JAAS cert login, probe,
// prometheus scraping, and CA trust).
type CertIdentities struct {
	OperandCertSecretName    string
	OperandSubject           *pkix.Name
	OperatorSubject          *pkix.Name
	CASecretName             string
	CASecretKey              string
	PrometheusCertSecretName string
	PrometheusSubject        *pkix.Name // nil when prometheus cert is absent
}

// ResolveCertIdentities resolves all control-plane certificate identities for
// a Broker. When owningServiceName is non-empty, it uses the chain-of-trust
// per-service naming; otherwise it falls back to legacy global secrets.
func ResolveCertIdentities(cr *v1beta2.Broker, client rtclient.Client, owningServiceName string) (*CertIdentities, error) {
	if owningServiceName != "" {
		return resolveChainOfTrust(cr, client, owningServiceName)
	}
	return resolveLegacy(cr, client)
}

func resolveChainOfTrust(cr *v1beta2.Broker, client rtclient.Client, serviceName string) (*CertIdentities, error) {
	ids := &CertIdentities{
		OperandCertSecretName:    BrokerCertName(serviceName),
		CASecretName:             RootCertSecretName(serviceName),
		CASecretKey:              "tls.crt",
		PrometheusCertSecretName: PrometheusCertName(serviceName),
	}

	operandSecret, err := common.GetNamespacedSecret(client, ids.OperandCertSecretName, cr.Namespace)
	if err != nil {
		return nil, err
	}
	ids.OperandSubject, err = common.ExtractCertSubjectFromSecret(operandSecret)
	if err != nil {
		return nil, fmt.Errorf("failed to extract operand subject from certificate: %w", err)
	}

	operatorSecret, err := common.GetNamespacedSecret(client, OperatorCertName(serviceName), cr.Namespace)
	if err != nil {
		return nil, fmt.Errorf("failed to get operator cert secret: %w", err)
	}
	operatorCert, err := common.ExtractCertFromSecret(operatorSecret)
	if err != nil {
		return nil, fmt.Errorf("failed to extract operator cert: %w", err)
	}
	ids.OperatorSubject, err = common.ExtractCertSubject(operatorCert)
	if err != nil {
		return nil, fmt.Errorf("failed to extract operator subject: %w", err)
	}

	ids.PrometheusSubject = resolvePrometheusSubject(client, ids.PrometheusCertSecretName, cr.Namespace)
	return ids, nil
}

func resolveLegacy(cr *v1beta2.Broker, client rtclient.Client) (*CertIdentities, error) {
	ids := &CertIdentities{
		OperandCertSecretName: common.GetOperandCertSecretName(cr, client),
	}

	operandSecret, err := common.GetNamespacedSecret(client, ids.OperandCertSecretName, cr.Namespace)
	if err != nil {
		return nil, err
	}
	ids.OperandSubject, err = common.ExtractCertSubjectFromSecret(operandSecret)
	if err != nil {
		return nil, fmt.Errorf("failed to extract operand subject from certificate: %w", err)
	}

	caCertSecret, err := common.GetOperatorCASecret(client)
	if err != nil {
		return nil, fmt.Errorf("failed to get operator ca secret: %w", err)
	}
	ids.CASecretKey, err = common.GetOperatorCASecretKey(client, caCertSecret)
	if err != nil {
		return nil, fmt.Errorf("failed to get operator ca secret key: %w", err)
	}
	ids.CASecretName = common.GetOperatorCASecretName()

	operatorCert, err := common.GetOperatorClientCertificate(client, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get operator client cert: %w", err)
	}
	ids.OperatorSubject, err = common.ExtractCertSubject(operatorCert)
	if err != nil {
		return nil, fmt.Errorf("failed to extract operator subject from client cert: %w", err)
	}

	ids.PrometheusCertSecretName = common.GetPrometheusCertSecretName(cr, client)
	ids.PrometheusSubject = resolvePrometheusSubject(client, ids.PrometheusCertSecretName, cr.Namespace)
	return ids, nil
}

func resolvePrometheusSubject(client rtclient.Client, secretName, namespace string) *pkix.Name {
	secret, err := common.GetNamespacedSecret(client, secretName, namespace)
	if err != nil {
		return nil
	}
	subject, err := common.ExtractCertSubjectFromSecret(secret)
	if err != nil {
		return nil
	}
	return subject
}
