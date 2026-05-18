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

package certutil

import (
	"fmt"
	"strings"

	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/common"
	corev1 "k8s.io/api/core/v1"
)

const (
	Cert_annotation_key   = "cert-manager.io/issuer-name"
	Bundle_annotation_key = "trust.cert-manager.io/hash"
	Console_web_prefix    = "webconfig.bindings.artemis."

	// Suffix of provided secrets that contain tls.crt and tls.key items in PEM format.
	// Secrets with this suffix are not validated. It is useful for secrets created from
	// annotations because they typically won't exist till the annotations are processed
	// and the secret is 'provided' by some other controller.
	Cert_provided_secret_suffix = "-ptls"
)

var defaultKeyStorePassword = "password"

type SslArguments struct {
	KeyStoreType       string
	KeyStorePath       string
	KeyStorePassword   *string
	TrustStoreType     string
	TrustStorePath     string
	TrustStorePassword *string
	PemCfgs            []string
	IsConsole          bool
	NeedClientAuth     bool
}

func (s *SslArguments) ToSystemProperties() string {
	sslFlags := ""

	if s.KeyStorePath != "" {
		if s.KeyStoreType == "PEM" {
			ksPath := "/etc/secret-" + CfgToSecretName(s.KeyStorePath) + "/" + s.KeyStorePath
			sslFlags = "-D" + Console_web_prefix + "keyStorePath=" + ksPath
		} else {
			sslFlags = "-D" + Console_web_prefix + "keyStorePath=" + s.KeyStorePath
		}
	}

	if s.KeyStorePassword != nil {
		sslFlags = sslFlags + " -D" + Console_web_prefix + "keyStorePassword=" + *s.KeyStorePassword
	}

	if s.KeyStoreType == "PEM" {
		sslFlags = sslFlags + " -D" + Console_web_prefix + "keyStoreType=PEMCFG"
	} else if s.KeyStoreType != "" {
		sslFlags = sslFlags + " -D" + Console_web_prefix + "keyStoreType=" + s.KeyStoreType
	}

	if s.TrustStorePath != "" {
		sslFlags = sslFlags + " -D" + Console_web_prefix + "trustStorePath=" + s.TrustStorePath
	}
	if s.TrustStorePassword != nil {
		sslFlags = sslFlags + " -D" + Console_web_prefix + "trustStorePassword=" + *s.TrustStorePassword
	}
	if s.TrustStoreType != "" {
		sslFlags = sslFlags + " -D" + Console_web_prefix + "trustStoreType=" + s.TrustStoreType
	}
	if s.NeedClientAuth {
		sslFlags = sslFlags + " -D" + Console_web_prefix + "clientAuth=true"
	}

	sslFlags = sslFlags + " -D" + Console_web_prefix + "uri=" + getConsoleUri()

	return sslFlags
}

func getConsoleUri() string {
	return "https://FQ_HOST_NAME:8161"
}

func (s *SslArguments) ToFlags() string {
	sslFlags := ""

	if s.IsConsole {
		sslFlags = sslFlags + " " + "--ssl-key" + " " + s.KeyStorePath
		if s.KeyStorePassword != nil {
			sslFlags = sslFlags + " " + "--ssl-key-password" + " " + *s.KeyStorePassword
		}
		if s.TrustStorePath != "" {
			sslFlags = sslFlags + " " + "--ssl-trust" + " " + s.TrustStorePath
		}
		if s.TrustStorePassword != nil {
			sslFlags = sslFlags + " " + "--ssl-trust-password" + " " + *s.TrustStorePassword
		}
		return sslFlags
	}

	sep := "\\/"
	sslFlags = "sslEnabled=true"
	if s.KeyStorePath != "" {
		if s.KeyStoreType == "PEM" {
			ksPath := "/etc/secret-" + CfgToSecretName(s.KeyStorePath) + "/" + s.KeyStorePath
			sslFlags = sslFlags + ";" + "keyStorePath=" + strings.ReplaceAll(ksPath, "/", sep)
		} else {
			sslFlags = sslFlags + ";" + "keyStorePath=" + s.KeyStorePath
		}
	}
	if s.KeyStorePassword != nil {
		sslFlags = sslFlags + ";" + "keyStorePassword=" + *s.KeyStorePassword
	}

	if s.KeyStoreType == "PEM" {
		sslFlags = sslFlags + ";" + "keyStoreType=PEMCFG"
	} else if s.KeyStoreType != "" {
		sslFlags = sslFlags + ";" + "keyStoreType=" + s.KeyStoreType
	}

	if s.TrustStorePath != "" {
		sslFlags = sslFlags + ";" + "trustStorePath=" + s.TrustStorePath
	}
	if s.TrustStorePassword != nil {
		sslFlags = sslFlags + ";" + "trustStorePassword=" + *s.TrustStorePassword
	}
	if s.TrustStoreType != "" {
		sslFlags = sslFlags + ";" + "trustStoreType=" + s.TrustStoreType
	}

	return sslFlags
}

func IsSecretFromCert(secret *corev1.Secret) (bool, bool) {

	if _, exist := secret.Data["keyStorePassword"]; exist {
		return false, true
	}

	if _, exist := secret.Data["trustStorePassword"]; exist {
		return false, true
	}

	if strings.HasSuffix(secret.Name, Cert_provided_secret_suffix) {
		return true, true
	}

	_, exist := secret.Annotations[Cert_annotation_key]
	if len(secret.Data) < 2 {
		return exist, false
	} else if _, ok := secret.Data["tls.crt"]; !ok {
		return exist, false
	} else if _, ok := secret.Data["tls.key"]; !ok {
		return exist, false
	}
	return exist, true
}

func isSecretFromBundle(secret *corev1.Secret) bool {
	_, exist := secret.Annotations[Bundle_annotation_key]
	return exist
}

func GetSslArgumentsFromSecret(sslSecret *corev1.Secret, trustStoreType string, trustSecret *corev1.Secret, isConsole bool) (*SslArguments, error) {
	sslArgs := SslArguments{
		IsConsole: isConsole,
	}

	isCertSecret, isValid := IsSecretFromCert(sslSecret)

	if isCertSecret && !isValid {
		return nil, fmt.Errorf("certificate secret not have correct keys")
	}

	if isCertSecret {
		//internally we use PEM to represent PEMCFG
		sslArgs.KeyStoreType = "PEM"
	}

	sep := "/"
	if !isConsole {
		sep = "\\/"
	}

	volumeDir := sep + "etc" + sep + sslSecret.Name + "-volume"

	if sslArgs.KeyStoreType == "PEM" {
		uniqueName := sslSecret.Name + ".pemcfg"
		sslArgs.KeyStorePath = uniqueName
		sslArgs.PemCfgs = []string{
			sslArgs.KeyStorePath,
			"/etc/" + sslSecret.Name + "-volume/tls.key",
			"/etc/" + sslSecret.Name + "-volume/tls.crt",
		}
	} else {
		// if it is the cert-secret, we throw an error
		if isCertSecret {
			return nil, fmt.Errorf("certificate only supports PEM keystore type, actual: %v", sslArgs.KeyStoreType)
		}

		// old user secret
		sslArgs.KeyStorePassword = &defaultKeyStorePassword
		sslArgs.KeyStorePath = volumeDir + sep + "broker.ks"
		if passwordString := string(sslSecret.Data["keyStorePassword"]); passwordString != "" {
			if !isConsole {
				passwordString = strings.ReplaceAll(passwordString, "/", sep)
			}
			sslArgs.KeyStorePassword = &passwordString
		}
		if keyPathString := string(sslSecret.Data["keyStorePath"]); keyPathString != "" {
			if !isConsole {
				keyPathString = strings.ReplaceAll(keyPathString, "/", sep)
			}
			sslArgs.KeyStorePath = keyPathString
		}
	}

	if trustSecret == nil {
		if !isCertSecret {
			//old user secret
			trustSecret = sslSecret
		} else {
			//user didn't specify truststore
			return &sslArgs, nil
		}
	}

	isBundleSecret := isSecretFromBundle(trustSecret)

	if isBundleSecret {
		if trustStoreType != "" {
			if trustStoreType != "PEMCA" {
				return nil, fmt.Errorf("ca bundle secret must have PEMCA trust store type")
			}
		}
		sslArgs.TrustStoreType = "PEMCA"
	} else {
		if trustStoreType != "" {
			sslArgs.TrustStoreType = trustStoreType
		}

	}

	trustVolumeDir := sep + "etc" + sep + trustSecret.Name + "-volume"

	if isBundleSecret {
		bundleName, bundleErr := common.FindFirstDotPemKey(trustSecret)
		if bundleErr != nil {
			return nil, bundleErr
		}
		sslArgs.TrustStorePath = trustVolumeDir + sep + bundleName
	} else {
		//old user Secret
		sslArgs.TrustStorePassword = &defaultKeyStorePassword
		sslArgs.TrustStorePath = trustVolumeDir + sep + "client.ts"
		if trustPassword := string(trustSecret.Data["trustStorePassword"]); trustPassword != "" {
			if !isConsole {
				trustPassword = strings.ReplaceAll(trustPassword, "/", sep)
			}
			sslArgs.TrustStorePassword = &trustPassword
		}
		if trustStorePath := string(trustSecret.Data["trustStorePath"]); trustStorePath != "" {
			if !isConsole {
				trustStorePath = strings.ReplaceAll(trustStorePath, "/", sep)
			}
			sslArgs.TrustStorePath = trustStorePath
		}
	}

	return &sslArgs, nil
}

func CfgToSecretName(cfgFileName string) string {
	return strings.ReplaceAll(cfgFileName, ".", "-")
}
