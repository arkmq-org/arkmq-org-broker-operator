package jolokia

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	v1beta2 "github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v1beta2"
	cot "github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/chain-of-trust"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/resources/environments"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/common"
	ctrl "sigs.k8s.io/controller-runtime"
	rtclient "sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	jolokiaPort    = "8778"
	jolokiaPath    = "/jolokia"
	requestTimeout = 3 * time.Second
)

// StatusClient is a purpose-built Jolokia client for the Broker reconciler.
// It resolves TLS configuration based on Broker ownership: if owned by a
// BrokerService, it uses the chain-of-trust per-service secrets; otherwise
// it falls back to the legacy global operator secrets.
type StatusClient struct {
	brokerName   string
	endpoint     string
	namespace    string
	serviceOwner string // empty = legacy global secrets
	client       rtclient.Client
}

func NewStatusClient(cr *v1beta2.Broker, client rtclient.Client) *StatusClient {
	ordinalFqdn := common.OrdinalFQDNS(cr.Name, cr.Namespace, 0)
	brokerName := environments.ResolveBrokerNameFromEnvs(cr.Spec.Env, cr.Name)

	return &StatusClient{
		brokerName:   brokerName,
		endpoint:     "https://" + ordinalFqdn + ":" + jolokiaPort + jolokiaPath,
		namespace:    cr.Namespace,
		serviceOwner: owningServiceName(cr),
		client:       client,
	}
}

func owningServiceName(cr *v1beta2.Broker) string {
	for _, ref := range cr.OwnerReferences {
		if ref.Kind == "BrokerService" {
			return ref.Name
		}
	}
	return ""
}

// GetStatus reads the broker status via the Jolokia REST API.
// Returns the raw JSON status string.
func (c *StatusClient) GetStatus() (string, error) {
	url := c.endpoint + "/read/org.apache.activemq.artemis:broker=%22" + c.brokerName + "%22/Status"

	httpClient := c.httpClient()

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "arkmq-org-broker-management")

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return "", fmt.Errorf("jolokia HTTP %d: %s", resp.StatusCode, resp.Status)
	}

	var raw map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return "", fmt.Errorf("failed to decode jolokia response: %w", err)
	}

	if status, ok := raw["status"].(float64); ok && int(status) != 200 {
		errMsg, _ := raw["error"].(string)
		return "", fmt.Errorf("jolokia status %d: %s", int(status), errMsg)
	}

	if value, ok := raw["value"]; ok && value != nil {
		return fmt.Sprintf("%v", value), nil
	}
	return "", nil
}

func (c *StatusClient) httpClient() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	tlsConfig := &tls.Config{
		InsecureSkipVerify: true,
	}

	if c.serviceOwner != "" {
		if err := c.configureChainOfTrust(tlsConfig); err != nil {
			ctrl.Log.V(1).Info("chain-of-trust TLS setup failed, falling back to insecure", "error", err)
		}
	} else {
		c.configureLegacy(tlsConfig)
	}

	transport.TLSClientConfig = tlsConfig
	return &http.Client{
		Transport: transport,
		Timeout:   requestTimeout,
	}
}

func (c *StatusClient) configureChainOfTrust(cfg *tls.Config) error {
	operatorCertSecret, err := common.GetNamespacedSecret(c.client, cot.OperatorCertName(c.serviceOwner), c.namespace)
	if err != nil {
		return fmt.Errorf("operator cert secret: %w", err)
	}
	cert, err := tls.X509KeyPair(operatorCertSecret.Data["tls.crt"], operatorCertSecret.Data["tls.key"])
	if err != nil {
		return fmt.Errorf("operator cert keypair: %w", err)
	}

	caSecret, err := common.GetNamespacedSecret(c.client, cot.RootCertSecretName(c.serviceOwner), c.namespace)
	if err != nil {
		return fmt.Errorf("root CA secret: %w", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caSecret.Data["tls.crt"]) {
		return fmt.Errorf("failed to parse root CA from %s", cot.RootCertSecretName(c.serviceOwner))
	}

	cfg.InsecureSkipVerify = false
	cfg.Certificates = []tls.Certificate{cert}
	cfg.RootCAs = pool
	return nil
}

func (c *StatusClient) configureLegacy(cfg *tls.Config) {
	if common.OperatorHasCertAndTrustBundle(c.client) {
		cfg.InsecureSkipVerify = false
		cfg.GetClientCertificate = func(cri *tls.CertificateRequestInfo) (*tls.Certificate, error) {
			return common.GetOperatorClientCertificate(c.client, cri)
		}
	}
	if rootCAs, err := common.GetRootCAs(c.client); err == nil {
		cfg.RootCAs = rootCAs
	}
}
