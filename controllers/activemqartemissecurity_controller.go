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
	"reflect"

	brokerv1beta1 "github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v1beta1"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/resources"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/resources/environments"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/resources/secrets"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/common"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/lsrcrs"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/random"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/selectors"
	"github.com/go-logr/logr"
	"gopkg.in/yaml.v2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ActiveMQArtemisSecurityReconciler reconciles a ActiveMQArtemisSecurity object
type ActiveMQArtemisSecurityReconciler struct {
	client.Client
	Scheme           *runtime.Scheme
	BrokerReconciler *ActiveMQArtemisReconciler
	log              logr.Logger
}

func NewActiveMQArtemisSecurityReconciler(client client.Client, scheme *runtime.Scheme, brokerReconciler *ActiveMQArtemisReconciler, logger logr.Logger) *ActiveMQArtemisSecurityReconciler {
	return &ActiveMQArtemisSecurityReconciler{
		Client:           client,
		Scheme:           scheme,
		BrokerReconciler: brokerReconciler,
		log:              logger,
	}
}

//+kubebuilder:rbac:groups=broker.amq.io,namespace=arkmq-org-broker-operator,resources=activemqartemissecurities,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=broker.amq.io,namespace=arkmq-org-broker-operator,resources=activemqartemissecurities/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=broker.amq.io,namespace=arkmq-org-broker-operator,resources=activemqartemissecurities/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the ActiveMQArtemisSecurity object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.10.0/pkg/reconcile
func (r *ActiveMQArtemisSecurityReconciler) Reconcile(ctx context.Context, request ctrl.Request) (ctrl.Result, error) {

	reqLogger := r.log.WithValues("Request.Namespace", request.Namespace, "Request.Name", request.Name, "Reconciling", "ActiveMQArtemisSecurity")

	instance := &brokerv1beta1.ActiveMQArtemisSecurity{}

	if err := r.Client.Get(context.TODO(), request.NamespacedName, instance); err != nil {
		if errors.IsNotFound(err) {
			//unregister the CR
			r.BrokerReconciler.RemoveBrokerConfigHandler(request.NamespacedName)
			// Setting err to nil to prevent requeue
			err = nil
			//clean the CR
			lsrcrs.DeleteLastSuccessfulReconciledCR(request.NamespacedName, "security", getLabels(instance), r.Client)
		} else {
			reqLogger.Error(err, "Reconcile errored thats not IsNotFound, requeuing request", "Request Namespace", request.Namespace, "Request Name", request.Name)
		}

		if err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: common.GetReconcileResyncPeriod()}, nil
	}

	toReconcile := true
	newHandler := &ActiveMQArtemisSecurityConfigHandler{
		instance,
		request.NamespacedName,
		r,
	}

	if securityHandler := GetBrokerConfigHandler(request.NamespacedName); securityHandler == nil {
		reqLogger.V(1).Info("Operator doesn't have the security handler, try retrive it from secret")
		if existingHandler := lsrcrs.RetrieveLastSuccessfulReconciledCR(request.NamespacedName, "security", r.Client, getLabels(instance)); existingHandler != nil {
			//compare resource version
			if existingHandler.Checksum == instance.ResourceVersion {
				reqLogger.V(2).Info("The incoming security CR is identical to stored CR, no reconcile")
				toReconcile = false
			}
		}
	} else {
		if reflect.DeepEqual(securityHandler, newHandler) {
			reqLogger.V(1).Info("Will not reconcile the same security config")
			return ctrl.Result{RequeueAfter: common.GetReconcileResyncPeriod()}, nil
		}
	}

	if err := r.BrokerReconciler.AddBrokerConfigHandler(request.NamespacedName, newHandler, toReconcile); err != nil {
		reqLogger.Error(err, "failed to config security cr", "request", request.NamespacedName)
		return ctrl.Result{}, err
	}
	//persist the CR
	crstr, merr := common.ToJson(instance)
	if merr != nil {
		reqLogger.Error(merr, "failed to marshal cr")
	}

	instanceWithPasswords := newHandler.processCrPasswords()

	// remove superfluous data that can trip up the shell
	instanceWithPasswords.ObjectMeta = metav1.ObjectMeta{}

	data, err := yaml.Marshal(instanceWithPasswords)
	if err != nil {
		reqLogger.Error(merr, "failed to marshal cr with passwords")
	}

	lsrcrs.StoreLastSuccessfulReconciledCR(instance, instance.Name, instance.Namespace, "security",
		crstr, string(data), instance.ResourceVersion, getLabels(instance), r.Client, r.Scheme)

	return ctrl.Result{RequeueAfter: common.GetReconcileResyncPeriod()}, nil
}

type ActiveMQArtemisSecurityConfigHandler struct {
	SecurityCR     *brokerv1beta1.ActiveMQArtemisSecurity
	NamespacedName types.NamespacedName
	owner          *ActiveMQArtemisSecurityReconciler
}

func getLabels(cr *brokerv1beta1.ActiveMQArtemisSecurity) map[string]string {
	labelBuilder := selectors.LabelerData{}
	labelBuilder.Base(cr.Name).Suffix("sec").Generate()
	return labelBuilder.Labels()
}

func (r *ActiveMQArtemisSecurityConfigHandler) GetCRName() string {
	return r.SecurityCR.Name
}

func (r *ActiveMQArtemisSecurityConfigHandler) IsApplicableFor(brokerNamespacedName types.NamespacedName) bool {
	reqLogger := r.owner.log.WithValues("IsApplicableFor", brokerNamespacedName)

	applyTo := r.SecurityCR.Spec.ApplyToCrNames
	reqLogger.V(1).Info("applyTo", "len", len(applyTo), "sec", r.SecurityCR.Name)

	//currently security doesnt apply to other namespaces than its own
	if r.NamespacedName.Namespace != brokerNamespacedName.Namespace {
		reqLogger.V(2).Info("this security cr is not applicable for broker because it's not in my namespace")
		return false
	}
	if len(applyTo) == 0 {
		reqLogger.V(2).Info("this security cr is applicable for broker because no applyTo is configured")
		return true
	}
	for _, crName := range applyTo {
		reqLogger.V(2).Info("Going through applyTo", "crName", crName)
		if crName == "*" || crName == "" || crName == brokerNamespacedName.Name {
			reqLogger.V(1).Info("this security cr is applicable for broker as it's either match-all or match name")
			return true
		}
	}
	reqLogger.V(1).Info("all applyToCrNames checked, no match. Not applicable")
	return false
}

func (r *ActiveMQArtemisSecurityConfigHandler) processCrPasswords() *brokerv1beta1.ActiveMQArtemisSecurity {
	result := r.SecurityCR.DeepCopy()

	if len(result.Spec.LoginModules.PropertiesLoginModules) > 0 {
		for i, pm := range result.Spec.LoginModules.PropertiesLoginModules {
			if len(pm.Users) > 0 {
				for j, user := range pm.Users {
					if user.Password == nil {
						result.Spec.LoginModules.PropertiesLoginModules[i].Users[j].Password = r.getPassword("security-properties-"+pm.Name, user.Name)
					}
				}
			}
		}
	}

	if len(result.Spec.LoginModules.KeycloakLoginModules) > 0 {
		for _, pm := range result.Spec.LoginModules.KeycloakLoginModules {
			keycloakSecretName := "security-keycloak-" + pm.Name
			if pm.Configuration.ClientKeyStore != nil {
				if pm.Configuration.ClientKeyPassword == nil {
					pm.Configuration.ClientKeyPassword = r.getPassword(keycloakSecretName, "client-key-password")
				}
				if pm.Configuration.ClientKeyStorePassword == nil {
					pm.Configuration.ClientKeyStorePassword = r.getPassword(keycloakSecretName, "client-key-store-password")
				}
			}
			if pm.Configuration.TrustStore != nil {
				if pm.Configuration.TrustStorePassword == nil {
					pm.Configuration.TrustStorePassword = r.getPassword(keycloakSecretName, "trust-store-password")
				}
			}
			//need to process pm.Configuration.Credentials too. later.
			if len(pm.Configuration.Credentials) > 0 {
				for i, kv := range pm.Configuration.Credentials {
					if kv.Value == nil {
						pm.Configuration.Credentials[i].Value = r.getPassword(keycloakSecretName, "credentials-"+kv.Key)
					}
				}
			}
		}
	}
	return result
}

func (r *ActiveMQArtemisSecurityConfigHandler) GetDefaultLabels() map[string]string {
	defaultLabelData := selectors.LabelerData{}
	defaultLabelData.Base(r.SecurityCR.Name).Suffix("app").Generate()
	return defaultLabelData.Labels()

}

// retrive value from secret, generate value if not exist.
func (r *ActiveMQArtemisSecurityConfigHandler) getPassword(secretName string, key string) *string {
	//check if the secret exists.
	namespacedName := types.NamespacedName{
		Name:      secretName,
		Namespace: r.NamespacedName.Namespace,
	}
	// Attempt to retrieve the secret
	stringDataMap := make(map[string][]byte)

	secretDefinition := secrets.NewSecret(namespacedName, stringDataMap, r.GetDefaultLabels())

	if err := resources.Retrieve(namespacedName, r.owner.Client, secretDefinition); err != nil {
		if errors.IsNotFound(err) {
			//create the secret
			resources.Create(r.SecurityCR, r.owner.Client, r.owner.Scheme, secretDefinition)
		}
	} else {
		r.owner.log.V(2).Info("Found secret " + secretName)

		if elem, ok := secretDefinition.Data[key]; ok {
			//the value exists
			value := string(elem)
			return &value
		}
	}
	//now need generate value
	value := random.GenerateRandomString(8)
	//update the secret
	if secretDefinition.Data == nil {
		secretDefinition.Data = make(map[string][]byte)
	}
	secretDefinition.Data[key] = []byte(value)
	r.owner.log.V(1).Info("Updating secret", "secret", namespacedName.Name)
	if err := resources.Update(r.owner.Client, secretDefinition); err != nil {
		r.owner.log.Error(err, "failed to update secret", "secret", secretName)
	}
	return &value
}

func (r *ActiveMQArtemisSecurityConfigHandler) Config(initContainers []corev1.Container, outputDirRoot string, yacfgProfileVersion string, yacfgProfileName string) (value []string) {
	r.owner.log.V(1).Info("Reconciling", "cr", r.SecurityCR.Name)
	outputDir := outputDirRoot + "/security"
	var configCmds = []string{"echo \"making dir " + outputDir + "\"", "mkdir -p " + outputDir}
	filePath := outputDir + "/security-config.yaml"
	securitySecretVolumeName := "secret-security-" + r.SecurityCR.Name + "-volume"
	cmdPersistCRAsYaml := "cp /etc/" + securitySecretVolumeName + "/Data " + filePath
	r.owner.log.V(2).Info("get the command", "value", cmdPersistCRAsYaml)
	configCmds = append(configCmds, cmdPersistCRAsYaml)
	configCmds = append(configCmds, "/opt/amq-broker/script/cfg/config-security.sh")
	envVarName := "SECURITY_CFG_YAML"
	envVar := corev1.EnvVar{
		Name:      envVarName,
		Value:     filePath,
		ValueFrom: nil,
	}
	environments.Create(initContainers, &envVar)

	envVarName = "YACFG_PROFILE_VERSION"
	envVar = corev1.EnvVar{
		Name:      envVarName,
		Value:     yacfgProfileVersion,
		ValueFrom: nil,
	}
	environments.Create(initContainers, &envVar)

	envVarName = "YACFG_PROFILE_NAME"
	envVar = corev1.EnvVar{
		Name:      envVarName,
		Value:     yacfgProfileName,
		ValueFrom: nil,
	}
	environments.Create(initContainers, &envVar)

	r.owner.log.V(2).Info("returning config cmds", "value", configCmds)
	return configCmds
}

// SetupWithManager sets up the controller with the Manager.
func (r *ActiveMQArtemisSecurityReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&brokerv1beta1.ActiveMQArtemisSecurity{}).
		Complete(r)
}
