// Package version resolves broker image versions and validates Spec.Version against supported releases.
package version

import (
	"fmt"
	"os"

	osruntime "runtime"

	v1beta2 "github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v1beta2"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/common"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/version"
	"github.com/blang/semver/v4"
	"github.com/pkg/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

func DetermineCompactVersionToUse(customResource *v1beta2.Broker) (string, error) {
	log := ctrl.Log.WithName("util_common")
	resolvedFullVersion, err := ResolveBrokerVersionFromCR(customResource)
	if err != nil {
		log.Error(err, "failed to determine broker version from cr")
		return "", err
	}
	compactVersionToUse := version.CompactActiveMQArtemisVersion(resolvedFullVersion)

	return compactVersionToUse, nil
}

func ResolveBrokerVersionFromCR(cr *v1beta2.Broker) (string, error) {
	if cr.Spec.Version != "" {
		_, verr := semver.ParseTolerant(cr.Spec.Version)
		if verr != nil {
			return "", verr
		}
	}

	result := common.ResolveBrokerVersion(version.SupportedActiveMQArtemisSemanticVersions(), cr.Spec.Version)
	if result == nil {
		return "", errors.Errorf("did not find a matching broker in the supported list for %v", cr.Spec.Version)
	}
	return result.String(), nil
}

func determineImageToUse(customResource *v1beta2.Broker, imageTypeKey string) string {
	log := ctrl.Log.WithName("util_common")
	found := false
	imageName := ""
	compactVersionToUse, _ := DetermineCompactVersionToUse(customResource)

	genericRelatedImageEnvVarName := common.ImageNamePrefix + imageTypeKey + "_" + compactVersionToUse
	archSpecificRelatedImageEnvVarName := genericRelatedImageEnvVarName
	if osruntime.GOARCH == "arm64" || osruntime.GOARCH == "s390x" || osruntime.GOARCH == "ppc64le" {
		archSpecificRelatedImageEnvVarName = genericRelatedImageEnvVarName + "_" + osruntime.GOARCH
	}
	imageName, found = os.LookupEnv(archSpecificRelatedImageEnvVarName)
	log.V(1).Info("DetermineImageToUse", "env", archSpecificRelatedImageEnvVarName, "imageName", imageName)

	if !found {
		imageName, found = os.LookupEnv(genericRelatedImageEnvVarName)
		log.V(1).Info("DetermineImageToUse - from generic", "env", genericRelatedImageEnvVarName, "imageName", imageName)
	}

	if !found {
		imageName = version.DefaultImageName(archSpecificRelatedImageEnvVarName)
		log.V(1).Info("DetermineImageToUse - from default", "env", archSpecificRelatedImageEnvVarName, "imageName", imageName)
	}

	return imageName
}

func ResolveImage(customResource *v1beta2.Broker, key string) string {
	var imageName string

	if key == common.InitImageKey && IsLockedDown(customResource.Spec.DeploymentPlan.InitImage) {
		imageName = customResource.Spec.DeploymentPlan.InitImage
	} else if key == common.BrokerImageKey && IsLockedDown(customResource.Spec.DeploymentPlan.Image) {
		imageName = customResource.Spec.DeploymentPlan.Image
	} else {
		imageName = determineImageToUse(customResource, key)
	}
	return imageName
}

func IsLockedDown(imageAttribute string) bool {
	return imageAttribute != "placeholder" && imageAttribute != ""
}

func ValidateBrokerImageVersion(customResource *v1beta2.Broker) *metav1.Condition {
	_, err := ResolveBrokerVersionFromCR(customResource)
	if err != nil {
		return &metav1.Condition{
			Type:    v1beta2.ValidConditionType,
			Status:  metav1.ConditionFalse,
			Reason:  v1beta2.ValidConditionInvalidVersionReason,
			Message: fmt.Sprintf(".Spec.Version does not resolve to a supported broker version, reason %v", err),
		}
	}

	if IsLockedDown(customResource.Spec.DeploymentPlan.Image) {
		if customResource.Spec.Version != "" {
			if !version.IsSupportedActiveMQArtemisVersion(customResource.Spec.Version) {
				return &metav1.Condition{
					Type:    v1beta2.ValidConditionType,
					Status:  metav1.ConditionUnknown,
					Reason:  v1beta2.ValidConditionUnknownReason,
					Message: common.NotSupportedImageVersionMessage,
				}
			}
		} else {
			return &metav1.Condition{
				Type:    v1beta2.ValidConditionType,
				Status:  metav1.ConditionUnknown,
				Reason:  v1beta2.ValidConditionUnknownReason,
				Message: common.UnkonwonImageVersionMessage,
			}
		}
	}

	return nil
}
