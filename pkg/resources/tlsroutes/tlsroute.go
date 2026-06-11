package tlsroutes

import (
	"github.com/arkmq-org/arkmq-org-broker-operator/api/v1beta2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

// NewTLSRouteForCR builds a Gateway API TLSRoute (passthrough) for SSL-enabled
// acceptors or the console. routeName is the desired object name; using a
// caller-supplied name lets the reconciler keep naming consistent with its
// other resource types.
func NewTLSRouteForCR(existing *gatewayv1.TLSRoute, namespacedName types.NamespacedName, labels map[string]string, routeName string, targetServiceName string, port int32, ingressHost string, gateway *v1beta2.GatewayConfig) *gatewayv1.TLSRoute {

	hostname := gatewayv1.Hostname(ingressHost)
	svcName := gatewayv1.ObjectName(targetServiceName)
	portNum := gatewayv1.PortNumber(port)

	parentRef := gatewayv1.ParentReference{
		Name: gatewayv1.ObjectName(gateway.ParentRef.Name),
	}
	if gateway.ParentRef.Namespace != "" {
		gwNs := gatewayv1.Namespace(gateway.ParentRef.Namespace)
		parentRef.Namespace = &gwNs
	}

	desired := &gatewayv1.TLSRoute{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "gateway.networking.k8s.io/v1alpha2",
			Kind:       "TLSRoute",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      routeName,
			Namespace: namespacedName.Namespace,
			Labels:    labels,
		},
		Spec: gatewayv1.TLSRouteSpec{
			CommonRouteSpec: gatewayv1.CommonRouteSpec{
				ParentRefs: []gatewayv1.ParentReference{parentRef},
			},
			Hostnames: []gatewayv1.Hostname{hostname},
			Rules: []gatewayv1.TLSRouteRule{
				{
					BackendRefs: []gatewayv1.BackendRef{
						{
							BackendObjectReference: gatewayv1.BackendObjectReference{
								Name: svcName,
								Port: &portNum,
							},
						},
					},
				},
			},
		},
	}

	if existing != nil {
		existing.Spec = desired.Spec
		existing.Labels = labels
		return existing
	}

	return desired
}
