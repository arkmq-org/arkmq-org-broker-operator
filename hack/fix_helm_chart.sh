#!/bin/bash

# set byte-value collation for consistent sorting behavior
LC_COLLATE=C

version=$1
dir=$2
YQ=${3:-yq}

# version
$YQ -i ".version=\"${version}\"" ${dir}/Chart.yaml
$YQ -i ".appVersion=\"${version}\"" ${dir}/Chart.yaml

# cluster scoped, namespaces
sed -i 's~kind: Role~kind: {{ if .Values.clusterScoped }}Cluster{{ end }}Role~' ${dir}/templates/operator-rbac.yaml
$YQ -i ".clusterScoped=true" ${dir}/values.yaml
$YQ -i ".watchNamespaces=[]" ${dir}/values.yaml
sed -i -z 's~WATCH_NAMESPACE\n          valueFrom:\n            fieldRef:\n              fieldPath: metadata.namespace~WATCH_NAMESPACE\n          value: {{ if .Values.clusterScoped }}{{ join "," .Values.watchNamespaces | quote }}{{ else }}{{ .Release.Namespace | quote }}{{ end }}~' \
${dir}/templates/deployment.yaml

# related image prefix
$YQ -i '.controllerManager.manager.relatedImages.activemqArtemisBrokerInitRepository="quay.io/arkmq-org/arkmq-org-broker-init"' ${dir}/values.yaml
$YQ -i '.controllerManager.manager.relatedImages.activemqArtemisBrokerKubernetesRepository="quay.io/arkmq-org/arkmq-org-broker-kubernetes"' ${dir}/values.yaml
$YQ -i '.controllerManager.manager.relatedImages += (.controllerManager.manager.env | with_entries(select(.key | test("^relatedImageA.+"))) | with_entries(.key |= sub("relatedImageA","a")) | with_entries(.value |= {"digest": sub("quay.io.*@","")}))' ${dir}/values.yaml
$YQ -i 'del(.controllerManager.manager.env)' ${dir}/values.yaml
sed -i -E 's~\.Values\.controllerManager\.manager\.env\.relatedImageA(ctivemqArtemisBrokerInit.+)~(printf "%s@%s" .Values.controllerManager.manager.relatedImages.activemqArtemisBrokerInitRepository .Values.controllerManager.manager.relatedImages.a\1.digest)~' ${dir}/templates/deployment.yaml
sed -i -E 's~\.Values\.controllerManager\.manager\.env\.relatedImageA(ctivemqArtemisBrokerKubernetes.+)~(printf "%s@%s" .Values.controllerManager.manager.relatedImages.activemqArtemisBrokerKubernetesRepository .Values.controllerManager.manager.relatedImages.a\1.digest)~' ${dir}/templates/deployment.yaml

# pack CRDs as template
crds="${dir}/templates/crds.yaml"
echo "{{- if .Values.crds.apply }}" > $crds
for file in ${dir}/crds/*.yaml; do
  $YQ '.metadata.annotations."helm.sh/resource-policy"="keep"' $file >> $crds
  echo "---" >> $crds
done
echo "{{- end }}" >> $crds
sed -i '/resource-policy/ i \{{- if .Values.crds.keep }}' $crds
sed -i '/resource-policy/ a \{{- end }}' $crds
rm -R ${dir}/crds/
$YQ -i ".crds.apply=true" ${dir}/values.yaml
$YQ -i ".crds.keep=true" ${dir}/values.yaml

# ENABLE_WEBHOOKS always false
$YQ -i 'del(.controllerManager.manager.env.enableWebhooks)' ${dir}/values.yaml
sed -i "s~.Values.controllerManager.manager.env.enableWebhooks~false~" ${dir}/templates/deployment.yaml

# imagePullPolicy (null preserves Kubernetes default behavior)
$YQ -i '.controllerManager.manager.image.pullPolicy=null' ${dir}/values.yaml
sed -i '/name: manager/a\        {{- with .Values.controllerManager.manager.image.pullPolicy }}\n        imagePullPolicy: {{ . }}\n        {{- end }}' ${dir}/templates/deployment.yaml

# pod scheduling fields
$YQ -i ".controllerManager.nodeSelector={}" ${dir}/values.yaml
$YQ -i ".controllerManager.tolerations=[]" ${dir}/values.yaml
$YQ -i ".controllerManager.topologySpreadConstraints=[]" ${dir}/values.yaml
sed -i '/terminationGracePeriodSeconds:/a\      topologySpreadConstraints: {{ .Values.controllerManager.topologySpreadConstraints | default list | toJson }}' ${dir}/templates/deployment.yaml
sed -i '/terminationGracePeriodSeconds:/a\      tolerations: {{ .Values.controllerManager.tolerations | default list | toJson }}' ${dir}/templates/deployment.yaml
sed -i '/terminationGracePeriodSeconds:/a\      nodeSelector: {{- toYaml .Values.controllerManager.nodeSelector | nindent 8 }}' ${dir}/templates/deployment.yaml