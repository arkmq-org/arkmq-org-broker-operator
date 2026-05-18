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

	brokerv1beta1 "github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v1beta1"
	ss "github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/resources/statefulsets"
	mgmt "github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/artemis"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/channels"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/common"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/jolokia"
	jc "github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/jolokia_client"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/lsrcrs"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/namer"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/selectors"
	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

type AddressDeployment struct {
	AddressResource brokerv1beta1.ActiveMQArtemisAddress
	//a 0-len array means all statefulsets
	SsTargetNameBuilders []SSInfoData
}

var namespacedNameToAddressName = make(map[types.NamespacedName]AddressDeployment)

// ActiveMQArtemisAddressReconciler reconciles a ActiveMQArtemisAddress object
type ActiveMQArtemisAddressReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	log    logr.Logger
}

func NewActiveMQArtemisAddressReconciler(client client.Client, scheme *runtime.Scheme, logger logr.Logger) *ActiveMQArtemisAddressReconciler {
	return &ActiveMQArtemisAddressReconciler{
		Client: client,
		Scheme: scheme,
		log:    logger,
	}
}

//+kubebuilder:rbac:groups=broker.amq.io,namespace=arkmq-org-broker-operator,resources=activemqartemisaddresses,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=broker.amq.io,namespace=arkmq-org-broker-operator,resources=activemqartemisaddresses/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=broker.amq.io,namespace=arkmq-org-broker-operator,resources=activemqartemisaddresses/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the ActiveMQArtemisAddress object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.10.0/pkg/reconcile
func (r *ActiveMQArtemisAddressReconciler) Reconcile(ctx context.Context, request ctrl.Request) (ctrl.Result, error) {
	reqLogger := r.log.WithValues("Request.Namespace", request.Namespace, "Request.Name", request.Name)

	addressInstance, lookupSucceeded := namespacedNameToAddressName[request.NamespacedName]
	// Fetch the ActiveMQArtemisAddress instance
	instance := &brokerv1beta1.ActiveMQArtemisAddress{}
	err := r.Get(context.TODO(), request.NamespacedName, instance)

	if err != nil {
		if errors.IsNotFound(err) {
			if lookupSucceeded {
				if addressInstance.AddressResource.Spec.RemoveFromBrokerOnDelete {
					err = r.deleteQueue(&addressInstance, request, r.Client)
					if err == nil {
						reqLogger.V(1).Info("Address and queue deleted")
					} else {
						reqLogger.Error(err, "Error deleting address and queue")
						return ctrl.Result{RequeueAfter: common.GetReconcileResyncPeriod()}, nil
					}
				}
				delete(namespacedNameToAddressName, request.NamespacedName)
				lsrcrs.DeleteLastSuccessfulReconciledCR(request.NamespacedName,
					"address", getAddressLabels(&addressInstance.AddressResource), r.Client)
				reqLogger.V(1).Info("Address resource deleted")
			} else {
				// the lookup for a not found ActiveMQArtemisAddress CR should
				// fail if the ActiveMQArtemisAddress CR is deleted before the
				// first reconcile loop or after a successful deletion
				reqLogger.Info("Address resource already deleted")
			}
			return ctrl.Result{}, nil
		} else {
			reqLogger.Error(err, "Error getting the request resource")
			return ctrl.Result{}, err
		}
	}

	addressDeployment := AddressDeployment{
		AddressResource:      *instance,
		SsTargetNameBuilders: r.createNameBuilders(instance),
	}

	if !lookupSucceeded {
		//check stored cr
		if existingCr := lsrcrs.RetrieveLastSuccessfulReconciledCR(request.NamespacedName, "address", r.Client, getAddressLabels(instance)); existingCr != nil {
			//compare resource version
			if existingCr.Checksum == instance.ResourceVersion {
				reqLogger.V(1).Info("The incoming address CR is identical to stored CR, don't do reconcile")
				//the namespacedNameToAddressName is empty after a restart
				namespacedNameToAddressName[request.NamespacedName] = addressDeployment
				return ctrl.Result{RequeueAfter: common.GetReconcileResyncPeriod()}, nil
			}
		}
	}

	err = r.createQueue(&addressDeployment, request, r.Client)
	if nil == err {
		namespacedNameToAddressName[request.NamespacedName] = addressDeployment
		crstr, merr := common.ToJson(instance)
		if merr != nil {
			reqLogger.Error(merr, "failed to marshal cr")
		}
		lsrcrs.StoreLastSuccessfulReconciledCR(instance, instance.Name, instance.Namespace, "address", crstr, "", instance.ResourceVersion, getAddressLabels(instance), r.Client, r.Scheme)
	} else {
		reqLogger.Error(err, "failed to create address resource, request will be requeued")
	}
	if err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{RequeueAfter: common.GetReconcileResyncPeriod()}, nil
}

func getAddressLabels(cr *brokerv1beta1.ActiveMQArtemisAddress) map[string]string {
	labelBuilder := selectors.LabelerData{}
	labelBuilder.Base(cr.Name).Suffix("addr").Generate()
	return labelBuilder.Labels()
}

type SSInfoData struct {
	NameBuilder namer.NamerData
	Labels      map[string]string
}

func createStatefulSetNameBuilder(crName string) SSInfoData {
	ssNameBuilder := namer.NamerData{}
	ssNameBuilder.Base(crName).Suffix("ss").Generate()
	ssLabelData := selectors.LabelerData{}
	ssLabelData.Base(crName).Suffix("app").Generate()

	return SSInfoData{
		NameBuilder: ssNameBuilder,
		Labels:      ssLabelData.Labels(),
	}
}

func (r *ActiveMQArtemisAddressReconciler) createNameBuilders(instance *brokerv1beta1.ActiveMQArtemisAddress) []SSInfoData {
	var nameBuilders []SSInfoData = nil
	for _, crName := range instance.Spec.ApplyToCrNames {
		if crName != "*" {
			builder := createStatefulSetNameBuilder(crName)
			r.log.V(2).Info("created a new name builder", "builder", builder, "buldername", builder.NameBuilder.Name())
			nameBuilders = append(nameBuilders, builder)
			r.log.V(2).Info("added one builder for "+crName, "builders", nameBuilders, "len", len(nameBuilders))
		} else {
			return nil
		}
	}
	r.log.V(1).Info("Created ss name builders for address "+instance.Namespace+"/"+instance.Name, "builders", nameBuilders)
	return nameBuilders
}

// SetupWithManager sets up the controller with the Manager.
func (r *ActiveMQArtemisAddressReconciler) SetupWithManager(mgr ctrl.Manager, ctx context.Context) error {
	go r.setupAddressObserver(mgr, ctx)
	return ctrl.NewControllerManagedBy(mgr).
		For(&brokerv1beta1.ActiveMQArtemisAddress{}).
		Owns(&corev1.Pod{}).
		Complete(r)
}

// This method deals with creating queues and addresses.
func (r *ActiveMQArtemisAddressReconciler) createQueue(instance *AddressDeployment, request ctrl.Request, client client.Client) error {

	r.log.V(1).Info("Creating ActiveMQArtemisAddress")

	var err error = nil
	artemisArray := r.getPodBrokers(instance, request, client)
	if nil != artemisArray {
		for _, a := range artemisArray {
			if nil == a {
				r.log.V(1).Info("Creating ActiveMQArtemisAddress artemisArray had a nil!")
				continue
			}
			err = createAddressResource(a, &instance.AddressResource, r.log)
			if err != nil {
				r.log.V(1).Info("Failed to create address resource", "failed broker", a)
				continue
			}
		}
	}

	if err == nil {
		r.log.V(1).Info("Successfully created resources on all brokers", "size", len(artemisArray))
	}

	return err
}

func createAddressResource(a *jc.JkInfo, addressRes *brokerv1beta1.ActiveMQArtemisAddress, log logr.Logger) error {
	//Now checking if create queue or address
	if addressRes.Spec.QueueName == nil || *addressRes.Spec.QueueName == "" {
		//create address
		routingType := ""
		if addressRes.Spec.RoutingType != nil {
			routingType = *addressRes.Spec.RoutingType
		}
		response, err := a.Artemis.CreateAddress(addressRes.Spec.AddressName, routingType)
		if nil != err {
			if mgmt.GetCreationError(response) == mgmt.ADDRESS_ALREADY_EXISTS {
				log.V(1).Info("Address already exists, no retry", "address", addressRes.Spec.AddressName)
				return nil
			} else {
				log.Error(err, "Error creating ActiveMQArtemisAddress", "address", addressRes.Spec.AddressName)
				return err
			}
		} else {
			log.V(1).Info("Created ActiveMQArtemisAddress for address " + addressRes.Spec.AddressName)
		}
	} else {
		log.V(1).Info("Queue name is not empty so create queue", "name", *addressRes.Spec.QueueName, "broker", a.IP)
		//first make sure address exists
		response, err := a.Artemis.CreateAddress(addressRes.Spec.AddressName, *addressRes.Spec.RoutingType)
		if nil != err && mgmt.GetCreationError(response) != mgmt.ADDRESS_ALREADY_EXISTS {
			log.Error(err, "Error creating ActiveMQArtemisAddress", "address", addressRes.Spec.AddressName)
			return err
		}

		defaultConfigurationManaged := true
		if addressRes.Spec.QueueConfiguration == nil {
			routingType := "MULTICAST"
			if addressRes.Spec.RoutingType != nil {
				routingType = *addressRes.Spec.RoutingType
			}

			addressRes.Spec.QueueConfiguration = &brokerv1beta1.QueueConfigurationType{
				RoutingType:          &routingType,
				ConfigurationManaged: &defaultConfigurationManaged,
			}
		} else if addressRes.Spec.QueueConfiguration.ConfigurationManaged == nil {
			addressRes.Spec.QueueConfiguration.ConfigurationManaged = &defaultConfigurationManaged
		}
		//create queue using queueconfig
		queueCfg, ignoreIfExists, err := GetQueueConfig(addressRes)
		if err != nil {
			log.Error(err, "Failed to get queue config json string")
			//here we return nil as no point to requeue reconcile again
			return nil
		}
		respData, err := a.Artemis.CreateQueueFromConfig(queueCfg, ignoreIfExists)
		if nil != err {
			if mgmt.GetCreationError(respData) == mgmt.QUEUE_ALREADY_EXISTS {
				log.V(2).Info("The queue already exists, updating", "queue", queueCfg)
				respData, err := a.Artemis.UpdateQueue(queueCfg)
				if err != nil {
					log.Error(err, "Failed to update queue", "details", respData)
				}
				return err
			}
			log.Error(err, "Creating ActiveMQArtemisAddress error for "+*addressRes.Spec.QueueName)
			return err
		} else {
			log.V(1).Info("Created ActiveMQArtemisAddress for " + *addressRes.Spec.QueueName)
		}
	}
	return nil
}

type AddressRetry struct {
	address string
	artemis []*mgmt.Artemis
	log     logr.Logger
}

func NewAddressRetry(addr string, a []*mgmt.Artemis, logger logr.Logger) *AddressRetry {
	return &AddressRetry{
		address: addr,
		artemis: a,
		log:     logger,
	}
}

func (ar *AddressRetry) addToDelete(a *mgmt.Artemis) {
	ar.artemis = append(ar.artemis, a)
}

func (ar *AddressRetry) safeDelete() {
	for _, a := range ar.artemis {
		ar.log.V(2).Info("Checking parent address for bindings " + ar.address)
		bindingsData, err := a.ListBindingsForAddress(ar.address)
		if nil == err {
			if bindingsData.Value == "" {
				ar.log.V(2).Info("No bindings found, removing " + ar.address)
				a.DeleteAddress(ar.address)
			} else {
				ar.log.V(2).Info("Bindings found, not removing", "address", ar.address, "bindings", bindingsData.Value)
			}
		} else {
			ar.log.Error(err, "failed to list bindings", "address", ar.address)
		}
	}
}

// This method deals with deleting queues and addresses.
func (r *ActiveMQArtemisAddressReconciler) deleteQueue(instance *AddressDeployment, request ctrl.Request, client client.Client) error {

	reqLogger := r.log.WithValues("Request.Namespace", request.Namespace, "Request.Name", request.Name)

	addressName := instance.AddressResource.Spec.AddressName

	queueName := ""
	if instance.AddressResource.Spec.QueueName != nil {
		queueName = *instance.AddressResource.Spec.QueueName
	}

	reqLogger.V(1).Info("Deleting ActiveMQArtemisAddress for queue " + addressName + "/" + queueName)

	var err error = nil
	artemisArray := r.getPodBrokers(instance, request, client)
	if nil != artemisArray {
		addressRetry := NewAddressRetry(addressName, make([]*mgmt.Artemis, 0), r.log.WithName("retry"))
		for _, a := range artemisArray {
			if queueName == "" {
				//delete address
				_, err = a.Artemis.DeleteAddress(addressName)
				if nil != err {
					reqLogger.Error(err, "Deleting ActiveMQArtemisAddress error", "address", addressName)
					break
				}
				reqLogger.V(1).Info("Deleted ActiveMQArtemisAddress for address " + addressName)
			} else {
				//delete queues
				var response *jolokia.ResponseData
				response, err = a.Artemis.DeleteQueue(queueName)
				if nil != err {
					if mgmt.GetCreationError(response) == mgmt.QUEUE_NOT_EXISTS {
						reqLogger.V(1).Info("Queue is already gone", "queue", queueName)
						err = nil
					} else {
						reqLogger.V(1).Info("Error deleting queue", "response", response)
						reqLogger.Error(err, "Deleting ActiveMQArtemisAddress error for queue "+queueName)
					}
					break
				} else {
					addressRetry.addToDelete(a.Artemis)
				}
			}
		}
		// we delete address after all queues are deleted
		addressRetry.safeDelete()
		reqLogger.V(1).Info("Deleted ActiveMQArtemisAddress for queue " + addressName + "/" + queueName)
	}

	return err
}

func (r *ActiveMQArtemisAddressReconciler) getPodBrokers(instance *AddressDeployment, request ctrl.Request, client client.Client) []*jc.JkInfo {
	reqLogger := r.log.WithValues("Request.Namespace", request.Namespace, "Request.Name", request.Name)
	reqLogger.V(2).Info("Getting Pod Brokers for address " + instance.AddressResource.Namespace + "/" + instance.AddressResource.Name)
	targetCrNamespacedNames := createTargetCrNamespacedNames(request.Namespace, instance.AddressResource.Spec.ApplyToCrNames, reqLogger)
	reqLogger.V(2).Info("target Cr names", "result", targetCrNamespacedNames)
	ssInfos := ss.GetDeployedStatefulSetNames(client, request.Namespace, targetCrNamespacedNames)

	return jc.GetBrokers(request.NamespacedName, ssInfos, client)
}

func createTargetCrNamespacedNames(namespace string, targetCrNames []string, log logr.Logger) []types.NamespacedName {
	var result []types.NamespacedName = nil
	for _, crName := range targetCrNames {
		result = append(result, types.NamespacedName{
			Namespace: namespace,
			Name:      crName,
		})
		if crName == "" || crName == "*" {
			log.V(2).Info("Found empty or * in target crName, return nil for all")
			return nil
		}
	}
	return result
}

func GetStatefulSetNameForPod(client client.Client, pod *types.NamespacedName, log logr.Logger) (string, int, map[string]string) {
	log.V(1).Info("Trying to find SS name for pod", "pod name", pod.Name, "pod ns", pod.Namespace)
	for crName, addressDeployment := range namespacedNameToAddressName {
		log.V(2).Info("checking address cr in stock", "cr", crName)
		if crName.Namespace != pod.Namespace {
			log.V(2).Info("this cr doesn't match pod's namespace", "cr's ns", crName.Namespace)
			continue
		}
		if len(addressDeployment.SsTargetNameBuilders) == 0 {
			log.V(2).Info("this cr doesn't have target specified, it will be applied to all")
			//deploy to all sts, need get from broker controller
			ssInfos := ss.GetDeployedStatefulSetNames(client, pod.Namespace, nil)
			if len(ssInfos) == 0 {
				log.V(2).Info("No statefulset found")
				continue
			}
			for _, info := range ssInfos {
				log.V(2).Info("checking if this ss belong", "ss", info.NamespacedName.Name)
				if _, ok, podSerial := namer.PodBelongsToStatefulset(pod, &info.NamespacedName); ok {
					log.V(2).Info("got a match", "ss", info.NamespacedName.Name, "podSerial", podSerial)
					return info.NamespacedName.Name, podSerial, info.Labels
				}
			}
			log.V(2).Info("no match at all")
			continue
		}
		//iterate and check the ss name
		log.V(2).Info("Now processing cr with applyToCrNames")
		for _, ssNameBuilder := range addressDeployment.SsTargetNameBuilders {
			ssName := ssNameBuilder.NameBuilder.Name()
			log.V(2).Info("checking one applyTo", "ss", ssName)
			//at this point the ss name space is sure the same
			ssNameSpace := types.NamespacedName{
				Name:      ssName,
				Namespace: pod.Namespace,
			}
			if _, ok, podSerial := namer.PodBelongsToStatefulset(pod, &ssNameSpace); ok {
				log.V(2).Info("yes this ssName match, returning results", "ssName", ssName, "podSerial", podSerial)
				return ssName, podSerial, ssNameBuilder.Labels
			}
		}
	}
	log.V(1).Info("all through, none found")
	return "", -1, nil
}

func (r *ActiveMQArtemisAddressReconciler) setupAddressObserver(mgr manager.Manager, ctx context.Context) {
	r.log.V(2).Info("Setting up address observer")

	kubeClient, err := kubernetes.NewForConfig(mgr.GetConfig())
	if err != nil {
		r.log.Error(err, "Error building kubernetes clientset")
	}

	observer := NewAddressObserver(kubeClient, mgr.GetClient(), mgr.GetScheme(), r.log)

	if err = observer.Run(channels.AddressListeningCh, ctx); err != nil {
		r.log.Error(err, "Error running controller")
	}
}
