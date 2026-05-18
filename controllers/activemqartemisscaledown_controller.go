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
	"os"
	"time"

	brokerv1beta1 "github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v1beta1"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/draincontroller"
	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	kubeinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var StopCh chan struct{}

var controllers map[string]*draincontroller.Controller = make(map[string]*draincontroller.Controller)

var kubeClient *kubernetes.Clientset

// ActiveMQArtemisScaledownReconciler reconciles a ActiveMQArtemisScaledown object
type ActiveMQArtemisScaledownReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	Config *rest.Config
	log    logr.Logger
}

func NewActiveMQArtemisScaledownReconciler(client client.Client, scheme *runtime.Scheme, config *rest.Config, logger logr.Logger) *ActiveMQArtemisScaledownReconciler {
	return &ActiveMQArtemisScaledownReconciler{
		Client: client,
		Scheme: scheme,
		Config: config,
		log:    logger,
	}
}

//+kubebuilder:rbac:groups=broker.amq.io,namespace=arkmq-org-broker-operator,resources=activemqartemisscaledowns,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=broker.amq.io,namespace=arkmq-org-broker-operator,resources=activemqartemisscaledowns/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=broker.amq.io,namespace=arkmq-org-broker-operator,resources=activemqartemisscaledowns/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the ActiveMQArtemisScaledown object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.10.0/pkg/reconcile
func (r *ActiveMQArtemisScaledownReconciler) Reconcile(ctx context.Context, request ctrl.Request) (ctrl.Result, error) {
	reqLogger := r.log.WithValues("Request.Namespace", request.Namespace, "Request.Name", request.Name, "Reconciling", "ActiveMQArtemisScaledown")

	// Fetch the ActiveMQArtemisScaledown instance
	instance := &brokerv1beta1.ActiveMQArtemisScaledown{}
	err := r.Client.Get(context.TODO(), request.NamespacedName, instance)
	if err != nil {
		if errors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			return ctrl.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return ctrl.Result{}, err
	}

	reqLogger.V(2).Info("scaling down", "localOnly:", instance.Spec.LocalOnly)

	kubeClient, err = kubernetes.NewForConfig(r.Config)
	if err != nil {
		reqLogger.Error(err, "Error building kubernetes clientset")
	}

	kubeInformerFactory, drainControllerInstance, isNewController := r.getDrainController(instance.Spec.LocalOnly, request.Namespace, kubeClient, instance)

	if isNewController {
		reqLogger.V(2).Info("Starting async factory...")
		go kubeInformerFactory.Start(*drainControllerInstance.GetStopCh())

		reqLogger.V(2).Info("Running drain controller async so multiple controllers can run...")
		go r.runDrainController(drainControllerInstance)
	}

	reqLogger.V(1).Info("OK, return result")
	return ctrl.Result{}, nil
}

func (r *ActiveMQArtemisScaledownReconciler) getDrainController(localOnly bool, namespace string, kubeClient *kubernetes.Clientset, instance *brokerv1beta1.ActiveMQArtemisScaledown) (kubeinformers.SharedInformerFactory, *draincontroller.Controller, bool) {
	var kubeInformerFactory kubeinformers.SharedInformerFactory
	var controllerInstance *draincontroller.Controller
	controllerKey := "*"
	if localOnly {
		if namespace == "" {
			bytes, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/namespace")
			if err != nil {
				r.log.Error(err, "Using --localOnly without --namespace, but unable to determine namespace")
			}
			namespace = string(bytes)
			r.log.V(2).Info("reading ns from file", "namespace", namespace)
		}
		controllerKey = namespace
	}
	if inst, ok := controllers[controllerKey]; ok {
		r.log.V(2).Info("Drain controller already exists", "namespace", namespace)
		inst.AddInstance(instance)
		return nil, nil, false
	}

	if localOnly {
		// localOnly means there is only one target namespace and it is the same as operator's
		r.log.V(2).Info("getting localOnly informer factory", "namespace", controllerKey)
		r.log.V(2).Info("Configured to only operate on StatefulSets", "namespace", namespace)
		kubeInformerFactory = kubeinformers.NewFilteredSharedInformerFactory(kubeClient, time.Second*30, namespace, nil)
	} else {
		r.log.V(2).Info("Creating informer factory to operate on StatefulSets across all namespaces")
		kubeInformerFactory = kubeinformers.NewSharedInformerFactory(kubeClient, time.Second*30)
	}

	r.log.V(2).Info("new drain controller...", "labels", instance.Labels)
	controllerInstance = draincontroller.NewController(controllerKey, kubeClient, kubeInformerFactory, namespace, r.Client, instance, r.log)
	controllers[controllerKey] = controllerInstance

	r.log.V(2).Info("Adding scaledown instance to controller", "controller", controllerInstance, "scaledown", instance)
	controllerInstance.AddInstance(instance)

	return kubeInformerFactory, controllerInstance, true
}

func (r *ActiveMQArtemisScaledownReconciler) runDrainController(controller *draincontroller.Controller) {
	if err := controller.Run(1); err != nil {
		r.log.Error(err, "Error running controller")
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *ActiveMQArtemisScaledownReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&brokerv1beta1.ActiveMQArtemisScaledown{}).
		Owns(&corev1.Pod{}).
		Complete(r)
}
