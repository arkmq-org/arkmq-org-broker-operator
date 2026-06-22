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

package main

import (
	"context"
	"flag"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/go-logr/logr"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	"k8s.io/client-go/rest"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"

	"fmt"
	goruntime "runtime"

	authorizationv1 "k8s.io/api/authorization/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	routev1 "github.com/openshift/api/route/v1"

	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/log"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/sdkk8sutil"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/common"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/version"

	brokerv1alpha1 "github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v1alpha1"
	brokerv1beta1 "github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v1beta1"
	brokerv1beta2 "github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v1beta2"
	brokerv2alpha1 "github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v2alpha1"
	brokerv2alpha2 "github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v2alpha2"
	brokerv2alpha3 "github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v2alpha3"
	brokerv2alpha4 "github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v2alpha4"
	brokerv2alpha5 "github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v2alpha5"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/controllers"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/appselector"
	//+kubebuilder:scaffold:imports
)

var (
	metricsHost       = "0.0.0.0"
	metricsPort int32 = 8383
)

var (
	//hard coded because the sdk version pkg is moved in internal package
	sdkVersion = "1.28.0"
	scheme     = runtime.NewScheme()
)

var setupLog logr.Logger

func printVersion() {
	setupLog.Info(fmt.Sprintf("Go Version: %s", goruntime.Version()))
	setupLog.Info(fmt.Sprintf("Go OS/Arch: %s/%s", goruntime.GOOS, goruntime.GOARCH))
	setupLog.Info(fmt.Sprintf("Version of operator-sdk: %v", sdkVersion))
	setupLog.Info(fmt.Sprintf("Version of the operator: %s %s", version.Version, version.BuildTimestamp))
	setupLog.Info(fmt.Sprintf("Supported ActiveMQArtemis Kubernetes Image Versions: %s", getSupportedBrokerVersions()))
}

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(routev1.AddToScheme(scheme))

	utilruntime.Must(brokerv2alpha1.AddToScheme(scheme))
	utilruntime.Must(brokerv2alpha2.AddToScheme(scheme))
	utilruntime.Must(brokerv2alpha3.AddToScheme(scheme))
	utilruntime.Must(brokerv2alpha4.AddToScheme(scheme))
	utilruntime.Must(brokerv2alpha5.AddToScheme(scheme))
	utilruntime.Must(brokerv1alpha1.AddToScheme(scheme))
	utilruntime.Must(brokerv1beta1.AddToScheme(scheme))
	utilruntime.Must(brokerv1beta2.AddToScheme(scheme))
	//+kubebuilder:scaffold:scheme
}

type TraceLogWarnings struct {
	Log logr.Logger
}

func (l *TraceLogWarnings) HandleWarningHeader(code int, agent string, message string) {
	l.Log.V(2).Info(message, "code", code, "agent", agent)
}

func main() {
	var metricsAddr string
	var enableLeaderElection bool
	var leaseDurationSeconds int64
	var renewDeadlineSeconds int64
	var retryPeriodSeconds int64
	var probeAddr string

	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.Int64Var(&leaseDurationSeconds, "lease-duration", 15, "LeaseDuration is the duration that non-leader candidates will wait to force acquire leadership. This is measured against time of last observed ack. Default is 15 seconds.")
	flag.Int64Var(&renewDeadlineSeconds, "renew-deadline", 10, "RenewDeadline is the duration that the acting controlplane will retry refreshing leadership before giving up. Default is 10 seconds.")
	flag.Int64Var(&retryPeriodSeconds, "retry-period", 2, "RetryPeriod is the duration the LeaderElector clients should wait between tries of actions. Default is 2 seconds.")
	opts := zap.Options{
		Development: true,
	}
	opts.BindFlags(flag.CommandLine)

	os.Args = append(os.Args, strings.Split(os.Getenv("ARGS"), " ")...)

	flag.Parse()

	logger := zap.New(zap.UseFlagOptions(&opts))

	// Exclude the logger with the name comparator because it logs the compared objects
	filteredLogSink := log.NewFilteredLogSink(logger.GetSink(), []string{"comparator"})

	ctrl.SetLogger(logger.WithSink(filteredLogSink))

	setupLog = ctrl.Log.WithName("setup")

	printVersion()

	// Get a config to talk to the apiserver
	cfg, err := config.GetConfig()
	if err != nil {
		setupLog.Error(err, "Error getting config for APIServer")
		os.Exit(1)
	}

	oprNamespace, err := sdkk8sutil.GetOperatorNamespace()
	if err != nil {
		setupLog.Error(err, "failed to get operator namespace, falling back to the environment DEFAULT_OPERATOR_NAMESPACE")
		oprNamespace = os.Getenv("DEFAULT_OPERATOR_NAMESPACE")
		if oprNamespace == "" {
			setupLog.Error(err, "failed to get operator namespace using the DEFAULT_OPERATOR_NAMESPACE env var")
			os.Exit(1)
		}
	}
	setupLog.Info("Got operator namespace", "operator ns", oprNamespace)

	watchNamespace, _ := sdkk8sutil.GetWatchNamespace()

	// Expose the operator's namespace and watchNamespace
	if err := os.Setenv("OPERATOR_NAMESPACE", oprNamespace); err != nil {
		setupLog.Error(err, "failed to set operator's namespace to env")
	}
	if err := os.Setenv("OPERATOR_WATCH_NAMESPACE", watchNamespace); err != nil {
		setupLog.Error(err, "failed to set operator's watch namespace to env")
	}

	leaseDuration := time.Duration(leaseDurationSeconds) * time.Second
	renewDeadline := time.Duration(renewDeadlineSeconds) * time.Second
	retryPeriod := time.Duration(retryPeriodSeconds) * time.Second

	mgrOptions := ctrl.Options{
		Scheme: scheme,
		Metrics: server.Options{
			BindAddress: fmt.Sprintf("%s:%d", metricsHost, metricsPort),
		},
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "d864aab0.amq.io",
		LeaseDuration:          &leaseDuration,
		RenewDeadline:          &renewDeadline,
		RetryPeriod:            &retryPeriod,
		Logger:                 setupLog,
	}

	mgrOptions.Client.WarningHandler.SuppressWarnings = true
	rest.SetDefaultWarningHandler(&TraceLogWarnings{Log: ctrl.Log})

	isLocal, watchList := common.ResolveWatchNamespaceForManager(oprNamespace, watchNamespace)
	if isLocal {
		setupLog.Info("setting up operator to watch local namespace")
		mgrOptions.Cache.DefaultNamespaces = map[string]cache.Config{
			oprNamespace: {}}
	} else {
		if watchList != nil {
			if len(watchList) == 1 {
				setupLog.Info("setting up operator to watch single namespace")
			} else {
				setupLog.Info("setting up operator to watch multiple namespaces", "namespace(s)", watchList)
			}
			nsMap := map[string]cache.Config{}
			for _, ns := range watchList {
				nsMap[ns] = cache.Config{}
			}
			mgrOptions.Cache.DefaultNamespaces = nsMap
		} else {
			setupLog.Info("setting up operator to watch all namespaces")
		}
	}

	defaultNamespaces := make([]string, 0, len(mgrOptions.Cache.DefaultNamespaces))
	for defaultNamespace := range mgrOptions.Cache.DefaultNamespaces {
		defaultNamespaces = append(defaultNamespaces, defaultNamespace)
	}

	setupLog.Info("Manager options",
		"Namespaces", defaultNamespaces,
		"MetricsBindAddress", mgrOptions.Metrics.BindAddress,
		"HealthProbeBindAddress", mgrOptions.HealthProbeBindAddress,
		"LeaderElection", mgrOptions.LeaderElection,
		"LeaderElectionID", mgrOptions.LeaderElectionID,
		"LeaseDuration", mgrOptions.LeaseDuration,
		"RenewDeadline", mgrOptions.RenewDeadline,
		"RetryPeriod", mgrOptions.RetryPeriod)

	mgr, err := ctrl.NewManager(cfg, mgrOptions)
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	// Ensure Namespace objects are cached for CEL evaluation in BrokerApp/BrokerService controllers
	// This allows CEL expressions to access namespace labels/annotations without live API calls
	// Check permissions first to avoid continuous errors from the informer trying to list/watch namespaces
	ctx := context.Background()
	clnt, err := client.New(cfg, client.Options{})
	if err != nil {
		setupLog.Error(err, "can't create client from config for permission check")
		os.Exit(1)
	}

	hasNamespacePermission := canListNamespaces(clnt, ctx)
	if !hasNamespacePermission {
		setupLog.Info("operator lacks permission to list namespaces - CEL expressions that reference appNamespace or serviceNamespace metadata will fail",
			"note", "to enable namespace-based CEL expressions, grant 'list' permission on namespaces resource")
	}
	// Set global namespace permission flag to avoid repeated failed API calls in CEL evaluation
	appselector.SetNamespacePermission(hasNamespacePermission)

	if hasNamespacePermission {
		_, err = mgr.GetCache().GetInformer(ctx, &corev1.Namespace{})
		if err != nil {
			setupLog.Error(err, "unable to setup namespace informer")
			os.Exit(1)
		}
		setupLog.Info("namespace informer configured for CEL evaluation")
	}

	// Set the service account name for the drainer pod
	// It will be broken without this as it won't have
	// permission to list the endpoints in drain.sh
	name := os.Getenv("POD_NAME")
	setupAccountName(clnt, context.TODO(), oprNamespace, name)

	isOpenshift, err := common.DetectOpenshiftWith(cfg)
	if err != nil {
		setupLog.Error(err, "can't determine api server type")
		os.Exit(1)
	}

	brokerReconciler := controllers.NewActiveMQArtemisReconciler(
		mgr,
		ctrl.Log.WithName("ActiveMQArtemisReconciler"),
		isOpenshift)

	if err = brokerReconciler.SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "ActiveMQArtemis")
		os.Exit(1)
	}

	brokerClusterReconciler := controllers.NewBrokerClusterReconciler(
		mgr,
		ctrl.Log.WithName("BrokerClusterReconciler"),
		isOpenshift)

	if err = brokerClusterReconciler.SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "BrokerCluster")
		os.Exit(1)
	}

	addressReconciler := controllers.NewActiveMQArtemisAddressReconciler(
		mgr.GetClient(),
		mgr.GetScheme(),
		ctrl.Log.WithName("ActiveMQArtemisAddressReconciler"))

	if err = addressReconciler.SetupWithManager(mgr, context.TODO()); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "ActiveMQArtemisAddress")
		os.Exit(1)
	}

	scaledownReconciler := controllers.NewActiveMQArtemisScaledownReconciler(
		mgr.GetClient(),
		mgr.GetScheme(),
		mgr.GetConfig(),
		ctrl.Log.WithName("ActiveMQArtemisScaledownReconciler"))

	if err = scaledownReconciler.SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "ActiveMQArtemisScaledown")
		os.Exit(1)
	}

	securityReconciler := controllers.NewActiveMQArtemisSecurityReconciler(
		mgr.GetClient(),
		mgr.GetScheme(),
		brokerReconciler,
		ctrl.Log.WithName("ActiveMQArtemisSecurityReconciler"))

	if err = securityReconciler.SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "ActiveMQArtemisSecurity")
		os.Exit(1)
	}

	serviceReconciler := controllers.NewBrokerServiceReconciler(
		mgr.GetClient(),
		mgr.GetScheme(),
		mgr.GetConfig(),
		ctrl.Log.WithName("BrokerServiceReconciler"))

	if err = serviceReconciler.SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "BrokerService")
		os.Exit(1)
	}

	appReconciler := controllers.NewBrokerAppReconciler(
		mgr.GetClient(),
		mgr.GetScheme(),
		mgr.GetConfig(),
		ctrl.Log.WithName("NewBrokerAppReconciler"))

	if err = appReconciler.SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "BrokerApp")
		os.Exit(1)
	}

	//+kubebuilder:scaffold:builder

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	setupLog.Info("Starting the manager")

	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}

func getSupportedBrokerVersions() string {
	allSupportVersions := make([]string, 0, 10)
	relatedImageEnvVarPrefix := "RELATED_IMAGE_BROKER_KUBERNETES_"
	// The full env var name should be relatedImageEnvVarPrefix + compactVersion
	for _, envLine := range os.Environ() {
		envPair := strings.Split(envLine, "=")
		if strings.HasPrefix(envPair[0], relatedImageEnvVarPrefix) {
			//try get compact version
			compactVersion := envPair[0][len(relatedImageEnvVarPrefix):]
			if fullVersion, ok := version.FullVersionFromCompactVersion[compactVersion]; ok {
				allSupportVersions = append(allSupportVersions, fullVersion)
			}
		}
	}
	sort.Strings(allSupportVersions)

	supportedProductVersions := ""
	for _, k := range allSupportVersions {
		supportedProductVersions += k
		supportedProductVersions += " "
	}

	return strings.TrimSpace(supportedProductVersions)
}

// canListNamespaces checks if the operator has permission to list namespaces cluster-wide.
// This is used to determine if we can set up a namespace informer without causing continuous errors.
func canListNamespaces(clnt client.Client, ctx context.Context) bool {
	sar := &authorizationv1.SelfSubjectAccessReview{
		Spec: authorizationv1.SelfSubjectAccessReviewSpec{
			ResourceAttributes: &authorizationv1.ResourceAttributes{
				Verb:     "list",
				Group:    "",
				Resource: "namespaces",
			},
		},
	}

	if err := clnt.Create(ctx, sar); err != nil {
		return false
	}

	return sar.Status.Allowed
}

func setupAccountName(clnt client.Client, ctx context.Context, ns, podname string) {
	pod := &corev1.Pod{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Pod",
		},
	}

	key := client.ObjectKey{Namespace: ns, Name: podname}
	err := clnt.Get(ctx, key, pod)
	if err != nil {
		setupLog.Error(err, "failed to get pod", "namespace", ns, "pod name", podname)
	} else {
		setupLog.Info("service account name: " + pod.Spec.ServiceAccountName)
		err = os.Setenv("SERVICE_ACCOUNT", pod.Spec.ServiceAccountName)
		if err != nil {
			setupLog.Error(err, "failed to set env variable")
		}
	}
}
