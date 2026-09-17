package templates

import (
	"bytes"
	_ "embed"
	"fmt"
	"sort"
	"text/template"
)

//go:embed security.properties.template
var securityTmpl string

//go:embed login_config.conf.template
var loginConfigTmpl string

//go:embed cert_users.properties.template
var certUsersTmpl string

//go:embed cert_roles.properties.template
var certRolesTmpl string

//go:embed app_cert_users.properties.template
var appCertUsersTmpl string

//go:embed app_cert_roles.properties.template
var appCertRolesTmpl string

//go:embed logging.properties.template
var loggingBytes []byte

//go:embed jolokia.properties.template
var jolokiaTmpl string

//go:embed pemcfg.properties.template
var pemcfgTmpl string

//go:embed ssl_header.yaml.template
var sslHeaderTmpl string

//go:embed broker_prometheus.yaml.template
var brokerPrometheusTmpl string

//go:embed service_prometheus.yaml.template
var servicePrometheusTmpl string

// mustParseWith parses content into a template set that already carries the
// {{define}} blocks of base, so content can {{template}} into them.
func mustParseWith(base, name, content string) *template.Template {
	return template.Must(template.Must(template.New("").Parse(base)).New(name).Parse(content))
}

func mustParse(name, content string) *template.Template {
	return mustParseWith(sslHeaderTmpl, name, content)
}

const (
	Security          = "security"
	Login             = "login_config"
	CertUsers         = "cert_users"
	CertRoles         = "cert_roles"
	AppCertUsers      = "app_cert_users"
	AppCertRoles      = "app_cert_roles"
	Jolokia           = "jolokia"
	PemCfg            = "pemcfg"
	BrokerPrometheus  = "broker_prometheus"
	ServicePrometheus = "service_prometheus"
)

var parsedTemplates = map[string]*template.Template{
	Security:          template.Must(template.New(Security).Parse(securityTmpl)),
	Login:             template.Must(template.New(Login).Parse(loginConfigTmpl)),
	CertUsers:         template.Must(template.New(CertUsers).Parse(certUsersTmpl)),
	CertRoles:         template.Must(template.New(CertRoles).Parse(certRolesTmpl)),
	AppCertUsers:      mustParseWith(certUsersTmpl, AppCertUsers, appCertUsersTmpl),
	AppCertRoles:      mustParseWith(certRolesTmpl, AppCertRoles, appCertRolesTmpl),
	Jolokia:           template.Must(template.New(Jolokia).Parse(jolokiaTmpl)),
	PemCfg:            template.Must(template.New(PemCfg).Parse(pemcfgTmpl)),
	BrokerPrometheus:  mustParse(BrokerPrometheus, brokerPrometheusTmpl),
	ServicePrometheus: mustParse(ServicePrometheus, servicePrometheusTmpl),
}

type SecurityConfig struct {
	MountPathRoot string
}

type LoginConfig struct {
	Realm         string
	MountPathRoot string
	CertUsersKey  string
	CertRolesKey  string
}

type CertUsersConfig struct {
	OperatorCN   string
	OperandCN    string
	PrometheusCN string
}

// AppIdentityEntry is one provisioned app's control-plane identity.
type AppIdentityEntry struct {
	Identity  string // <namespace>-<app>, matches metricsRole in capabilities.properties
	CNPattern string // regex-escaped cert CN, matched against the client cert DN
}

// AppCertUsersConfig renders the control-plane cert_users plus one line per app.
// The embedded CertUsersConfig feeds the shared "cert_users_base" define.
type AppCertUsersConfig struct {
	CertUsersConfig
	Apps []AppIdentityEntry
}

// AppCertRolesConfig renders the control-plane cert_roles plus the per-app
// metrics role membership.
type AppCertRolesConfig struct {
	Apps []AppIdentityEntry
}

type JolokiaConfig struct {
	CACertPath     string
	ServerCertPath string
	ServerKeyPath  string
}

type PemCfgConfig struct {
	Alias       string
	CertKeyPath string
	CertCrtPath string
}

type BrokerPrometheusConfig struct {
	PemCfgPath       string
	CATrustStorePath string
	BrokerName       string
}

type ServicePrometheusConfig struct {
	PemCfgPath       string
	CATrustStorePath string
	BrokerName       string
	Queues           []QueueEntry
}

type QueueEntry struct {
	Address     string
	RoutingType string
	Queue       string
}

func Render(name string, cfg any) ([]byte, error) {
	var buf bytes.Buffer
	if err := parsedTemplates[name].ExecuteTemplate(&buf, name, cfg); err != nil {
		return nil, fmt.Errorf("template %s: %w", name, err)
	}
	return buf.Bytes(), nil
}

func RenderCertRoles() []byte {
	data, err := Render(CertRoles, nil)
	if err != nil {
		// the template is embedded and takes no input, so this cannot fail
		panic(err)
	}
	return data
}

func RenderLogging() []byte {
	return loggingBytes
}

func RenderServicePrometheus(cfg ServicePrometheusConfig, appQueues map[string]bool) ([]byte, error) {
	if len(appQueues) > 0 {
		addresses := make([]string, 0, len(appQueues))
		for address := range appQueues {
			addresses = append(addresses, address)
		}
		sort.Strings(addresses)
		for _, address := range addresses {
			fqqn := splitFQQN(address)
			if len(fqqn) > 1 {
				cfg.Queues = append(cfg.Queues, QueueEntry{Address: fqqn[0], RoutingType: "multicast", Queue: fqqn[1]})
			} else {
				cfg.Queues = append(cfg.Queues, QueueEntry{Address: address, RoutingType: "anycast", Queue: address})
			}
		}
	}
	return Render(ServicePrometheus, cfg)
}

func splitFQQN(address string) []string {
	for i := 0; i < len(address)-1; i++ {
		if address[i] == ':' && address[i+1] == ':' {
			return []string{address[:i], address[i+2:]}
		}
	}
	return []string{address}
}
