package environments

import (
	"strconv"

	corev1 "k8s.io/api/core/v1"
)

const (
	NameEnvVar             = "AMQ_NAME"
	NameEnvVarDefaultValue = "amq-broker"

	// ExtraBrokerPropertiesEnvVar is an operator-consumed env var that the user
	// can set in Spec.Env with a comma-separated list of extra broker.properties paths
	// (files or directories). The operator appends it to its own -Dbroker.properties=
	// value via Kubernetes $(VAR_NAME) substitution so no reconcile-time parsing is needed.
	ExtraBrokerPropertiesEnvVar = "EXTRA_BROKER_PROPERTIES"
)

func ResolveBrokerNameFromEnvs(envs []corev1.EnvVar, defaultValue string) string {
	if len(envs) > 0 {
		for _, v := range envs {
			if v.Name == NameEnvVar {
				return v.Value
			}
		}
	}
	return defaultValue
}

// AddEnvVarForExtraBrokerProperties returns an env var with an empty default
// for EXTRA_BROKER_PROPERTIES. The empty default ensures the Kubernetes
// $(EXTRA_BROKER_PROPERTIES) reference in JDK_JAVA_OPTIONS always resolves
// (to an empty string) rather than being left as a literal token when the user
// has not set a custom value.
func AddEnvVarForExtraBrokerProperties() []corev1.EnvVar {
	return []corev1.EnvVar{{
		Name:  ExtraBrokerPropertiesEnvVar,
		Value: "",
	}}
}

func AddEnvVarForBasic(requireLogin string, journalType string, svcPingName string) []corev1.EnvVar {

	envVarArray := []corev1.EnvVar{
		{
			Name:      "AMQ_ROLE",
			Value:     "admin",
			ValueFrom: nil,
		},
		{
			Name:      NameEnvVar,
			Value:     NameEnvVarDefaultValue,
			ValueFrom: nil,
		},
		{
			Name:      "AMQ_TRANSPORTS",
			Value:     "",
			ValueFrom: nil,
		},
		{
			Name:      "AMQ_QUEUES",
			Value:     "",
			ValueFrom: nil,
		},
		{
			Name:      "AMQ_ADDRESSES",
			Value:     "",
			ValueFrom: nil,
		},
		{
			Name:      "AMQ_GLOBAL_MAX_SIZE",
			Value:     "100 mb",
			ValueFrom: nil,
		},
		{
			Name:      "AMQ_REQUIRE_LOGIN",
			Value:     requireLogin,
			ValueFrom: nil,
		},
		{
			Name:      "AMQ_EXTRA_ARGS",
			Value:     "--no-autotune",
			ValueFrom: nil,
		},
		{
			Name:      "AMQ_ANYCAST_PREFIX",
			Value:     "",
			ValueFrom: nil,
		},
		{
			Name:      "AMQ_MULTICAST_PREFIX",
			Value:     "",
			ValueFrom: nil,
		},
		{
			Name:      "POD_NAMESPACE",
			Value:     "",
			ValueFrom: nil,
		},
		{
			Name:      "AMQ_JOURNAL_TYPE",
			Value:     journalType,
			ValueFrom: nil,
		},
		{
			Name:      "TRIGGERED_ROLL_COUNT",
			Value:     "0",
			ValueFrom: nil,
		},
		{
			Name:      "PING_SVC_NAME",
			Value:     svcPingName,
			ValueFrom: nil,
		},
		{
			Name:      "OPENSHIFT_DNS_PING_SERVICE_PORT",
			Value:     "7800",
			ValueFrom: nil,
		},
	}

	return envVarArray
}

func AddEnvVarForPersistent(customResourceName string) []corev1.EnvVar {

	envVarArray := []corev1.EnvVar{
		{
			Name:      "AMQ_DATA_DIR",
			Value:     "/opt/" + customResourceName + "/data",
			ValueFrom: nil,
		},
		{
			Name:      "AMQ_DATA_DIR_LOGGING",
			Value:     "true",
			ValueFrom: nil,
		},
	}

	return envVarArray
}

func AddEnvVarForCluster(isClustered bool) []corev1.EnvVar {

	envVarArray := []corev1.EnvVar{
		{
			Name:      "AMQ_CLUSTERED",
			Value:     strconv.FormatBool(isClustered),
			ValueFrom: nil,
		},
	}

	return envVarArray
}

func AddEnvVarForJolokia(jolokiaAgentEnabled string) []corev1.EnvVar {

	envVarArray := []corev1.EnvVar{
		{
			Name:      "AMQ_ENABLE_JOLOKIA_AGENT",
			Value:     jolokiaAgentEnabled,
			ValueFrom: nil,
		},
	}

	return envVarArray
}

func AddEnvVarForManagement(managementRBACEnabled string) []corev1.EnvVar {

	envVarArray := []corev1.EnvVar{
		{
			Name:      "AMQ_ENABLE_MANAGEMENT_RBAC",
			Value:     managementRBACEnabled,
			ValueFrom: nil,
		},
	}

	return envVarArray
}

func AddEnvVarForMetricsPlugin(metricsPluginEnabled string) []corev1.EnvVar {

	envVarArray := []corev1.EnvVar{
		{
			Name:      "AMQ_ENABLE_METRICS_PLUGIN",
			Value:     metricsPluginEnabled,
			ValueFrom: nil,
		},
	}

	return envVarArray
}

// https://stackoverflow.com/questions/37334119/how-to-delete-an-element-from-a-slice-in-golang
func remove(s []corev1.EnvVar, i int) []corev1.EnvVar {
	s[i] = s[len(s)-1]
	// We do not need to put s[i] at the end, as it will be discarded anyway
	return s[:len(s)-1]
}

func BoolSyncCausedUpdateOn(containers []corev1.Container, envVarName string, updatedValue bool) *corev1.EnvVar {

	var retEnvVar *corev1.EnvVar = nil

	found := false
	needsUpdate := false

	// Find the existing values
	for _, v := range containers[0].Env {
		if v.Name == envVarName {
			found = true
			currentValue, _ := strconv.ParseBool(v.Value)
			if currentValue != updatedValue {
				needsUpdate = true
			}
		}
	}

	if !found || needsUpdate {
		retEnvVar = &corev1.EnvVar{
			Name:      envVarName,
			Value:     strconv.FormatBool(updatedValue),
			ValueFrom: nil,
		}
	}

	return retEnvVar
}

func StringSyncCausedUpdateOn(containers []corev1.Container, envVarName string, updatedValue string) *corev1.EnvVar {

	var retEnvVar *corev1.EnvVar = nil

	found := false
	needsUpdate := false

	// Find the existing values
	for _, v := range containers[0].Env {
		if v.Name == envVarName {
			found = true
			currentValue := v.Value
			if currentValue != updatedValue {
				needsUpdate = true
			}
		}
	}

	if !found || needsUpdate {
		retEnvVar = &corev1.EnvVar{
			Name:      envVarName,
			Value:     updatedValue,
			ValueFrom: nil,
		}
	}

	return retEnvVar
}

func TrackSecretCheckSumInRollCount(checkSum string, containers []corev1.Container) {

	newTriggeredRollCountEnvVar := corev1.EnvVar{
		Name:      "TRIGGERED_ROLL_COUNT",
		Value:     checkSum,
		ValueFrom: nil,
	}
	Update(containers, &newTriggeredRollCountEnvVar)
}

func Create(containers []corev1.Container, envVar *corev1.EnvVar) {

	for i := 0; i < len(containers); i++ {
		containers[i].Env = append(containers[i].Env, *envVar)
	}
}

func ReplaceOrAppend(envs []corev1.EnvVar, elems ...corev1.EnvVar) []corev1.EnvVar {

	var result []corev1.EnvVar = envs
	var found *corev1.EnvVar = nil
	for _, newElem := range elems {
		found = Find(envs, newElem.Name)
		if found != nil {
			found.Value = newElem.Value
		} else {
			result = append(result, newElem)
		}
	}
	return result
}

func CreateOrAppend(containers []corev1.Container, envVar *corev1.EnvVar) {

	for i, container := range containers {
		existing := RetrieveFrom(containers[i], envVar.Name)
		if existing == nil {
			containers[i].Env = append(container.Env, *envVar)
		} else {
			existing.Value += " " + envVar.Value
		}
	}
}

func Retrieve(containers []corev1.Container, envVarName string) *corev1.EnvVar {

	var retEnvVar *corev1.EnvVar = nil
	for i := 0; i < len(containers) && nil == retEnvVar; i++ {
		for j := len(containers[i].Env) - 1; j >= 0; j-- {
			if envVarName == containers[i].Env[j].Name {
				retEnvVar = &containers[i].Env[j]
				break
			}
		}
	}

	return retEnvVar
}

func RetrieveFrom(container corev1.Container, envVarName string) *corev1.EnvVar {
	return Find(container.Env, envVarName)
}

func Find(envs []corev1.EnvVar, envVarName string) *corev1.EnvVar {
	var retEnvVar *corev1.EnvVar = nil
	for i, envVar := range envs {
		if envVarName == envVar.Name {
			retEnvVar = &envs[i]
			break
		}
	}
	return retEnvVar
}

func Update(containers []corev1.Container, envVar *corev1.EnvVar) {

	for i := 0; i < len(containers); i++ {
		for j := len(containers[i].Env) - 1; j >= 0; j-- {
			if envVar.Name == containers[i].Env[j].Name {
				containers[i].Env[j] = *envVar
			}
		}
	}
}

func Delete(containers []corev1.Container, envVarName string) {

	for i := 0; i < len(containers); i++ {
		for j := len(containers[i].Env) - 1; j >= 0; j-- {
			if envVarName == containers[i].Env[j].Name {
				containers[i].Env = remove(containers[i].Env, j)
			}
		}
	}
}
