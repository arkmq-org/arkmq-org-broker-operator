#!/usr/bin/env bash
set -Eeuo pipefail
if [[ $(uname -s) == "Darwin" ]]; then
  shopt -s expand_aliases
  alias echo="gecho"; alias grep="ggrep"; alias sed="gsed"; alias date="gdate"
fi

{ # this ensures the entire script is downloaded #
NAMESPACE=""
CLUSTER=""
KUBECTL_INSTALLED=false
OC_INSTALLED=false
KUBE_CLIENT="kubectl"
OUT_DIR=""
SECRETS_OPT="hidden"

# sed non-printable text delimiter
SD=$(echo -en "\001") && readonly SD
# sed sensitive information filter expression
SE="s${SD}^\(\s*.*\password\s*:\s*\).*${SD}\1\[hidden\]${SD}; s${SD}^\(\s*.*\.key\s*:\s*\).*${SD}\1\[hidden\]${SD}" && readonly SE

error() {
  echo -n "$@" 1>&2 && exit 1
}

# bash version check
if [[ -z ${BASH_VERSINFO+x} ]]; then
  error "No bash version information available, aborting"
fi
if [[ "${BASH_VERSINFO[0]}" -lt 4 ]]; then
  error "You need bash version >= 4 to run the script"
fi

# kube client check
if [[ -x "$(command -v kubectl)" ]]; then
  KUBECTL_INSTALLED=true
else
  if [[ -x "$(command -v oc)" ]]; then
    OC_INSTALLED=true
    KUBE_CLIENT="oc"
  fi
fi

if [[ $OC_INSTALLED = false && $KUBECTL_INSTALLED = false ]]; then
  error "There is no kubectl or oc installed"
fi

# check kube connectivity
$KUBE_CLIENT version -o yaml --request-timeout=5s 1>/dev/null

readonly USAGE="
Usage: report.sh [options]

Interactive mode (auto-discovery - requires cluster admin rights):
  ./report.sh                   Discover all AMQ Broker clusters and select interactively.

Manual mode (works without cluster admin rights):
  --namespace=<string>          Kubernetes namespace.
  --cluster=<string>            AMQ Broker cluster name.

Optional:
  --secrets=(off|hidden|all)    Secret verbosity. Default is hidden, only the secret key will be reported.
  --out-dir=<string>            Script output directory.

Examples:
  # Interactive mode (requires cluster-wide permissions)
  ./report.sh

  # Manual mode (local script)
  ./report.sh --namespace=amq --cluster=broker-prod
  ./report.sh --namespace=amq --cluster=broker-prod --secrets=all --out-dir=~/Downloads

  # Manual mode (remote via curl - useful without cluster admin rights)
  bash <(curl -sLk \"https://raw.githubusercontent.com/aboucham/amq-broker-dump/refs/heads/main/report.sh\") --namespace=broker --cluster=broker
"
OPTSPEC=":-:"
while getopts "$OPTSPEC" optchar; do
  case "${optchar}" in
    -)
      case "${OPTARG}" in
        namespace=*)
          NAMESPACE=${OPTARG#*=} && readonly NAMESPACE
          ;;
        out-dir=*)
          OUT_DIR=${OPTARG#*=}
          OUT_DIR=${OUT_DIR//\~/$HOME} && readonly OUT_DIR
          ;;
        cluster=*)
          CLUSTER=${OPTARG#*=} && readonly CLUSTER
          ;;
        secrets=*)
          SECRETS_OPT=${OPTARG#*=} && readonly SECRETS_OPT
          ;;
        *)
          error "$USAGE"
          ;;
      esac;;
  esac
done
shift $((OPTIND-1))

if [[ -z $OUT_DIR ]]; then
  OUT_DIR="$(mktemp -d)"
fi

if [[ "$SECRETS_OPT" != "all" && "$SECRETS_OPT" != "off" && "$SECRETS_OPT" != "hidden" ]]; then
  echo "Unknown secrets verbosity level. Use one of 'off', 'hidden' or 'all'."
  echo " 'all' - secret keys and data values are reported"
  echo " 'hidden' - secrets with only data keys are reported"
  echo " 'off' - secrets are not reported at all"
  echo "Default value is 'hidden'"
  error "$USAGE"
fi

# Auto-discovery: if namespace and cluster not provided, try to discover all AMQ Broker instances
if [[ -z $NAMESPACE || -z $CLUSTER ]]; then
  echo ""
  echo "Auto-discovering AMQ Broker clusters..."
  echo ""

  # Try to get all ActiveMQArtemis and Broker instances across all namespaces
  AMQ_INSTANCES=$($KUBE_CLIENT get activemqartemises.broker.amq.io --all-namespaces --no-headers -o custom-columns=NAMESPACE:.metadata.namespace,NAME:.metadata.name 2>/dev/null)
  BROKER_INSTANCES=$($KUBE_CLIENT get brokers.broker.arkmq.org --all-namespaces --no-headers -o custom-columns=NAMESPACE:.metadata.namespace,NAME:.metadata.name 2>/dev/null)

  # Combine both instance types
  ALL_INSTANCES=$(echo -e "$AMQ_INSTANCES\n$BROKER_INSTANCES" | grep -v '^$')

  if [[ -z $ALL_INSTANCES ]]; then
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo "⚠️  INSUFFICIENT CLUSTER ADMIN RIGHTS DETECTED"
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo ""
    echo "Unable to auto-discover AMQ Broker clusters across all namespaces."
    echo "This typically means you don't have cluster-wide permissions."
    echo ""
    echo "Please use MANUAL MODE by specifying the namespace and cluster name:"
    echo ""
    echo "  bash <(curl -sLk \"https://raw.githubusercontent.com/aboucham/amq-broker-dump/refs/heads/main/report.sh\") \\"
    echo "    --namespace=<YOUR_NAMESPACE> \\"
    echo "    --cluster=<YOUR_CLUSTER_NAME>"
    echo ""
    echo "Example:"
    echo "  bash <(curl -sLk \"https://raw.githubusercontent.com/aboucham/amq-broker-dump/refs/heads/main/report.sh\") \\"
    echo "    --namespace=broker \\"
    echo "    --cluster=broker"
    echo ""
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    exit 1
  fi

  # Display found clusters
  echo "Found AMQ Broker clusters:"
  echo ""

  # Create array of instances
  mapfile -t INSTANCES < <(echo "$ALL_INSTANCES")

  # Display menu with index
  count=1
  for instance in "${INSTANCES[@]}"; do
    ns=$(echo "$instance" | awk '{print $1}')
    name=$(echo "$instance" | awk '{print $2}')
    echo "  $count) Namespace: $ns, Cluster: $name"
    count=$((count + 1))
  done

  echo ""

  # Read user selection
  read -p "Select cluster number (1-${#INSTANCES[@]}) or 'q' to quit: " selection

  # Handle quit
  if [[ "$selection" == "q" || "$selection" == "Q" ]]; then
    echo "Cancelled by user"
    exit 0
  fi

  # Validate selection is a number
  if [[ ! $selection =~ ^[0-9]+$ ]]; then
    error "Invalid selection. Please enter a number."
  fi

  # Validate selection is in range
  if [[ $selection -lt 1 || $selection -gt ${#INSTANCES[@]} ]]; then
    error "Invalid selection. Please enter a number between 1 and ${#INSTANCES[@]}."
  fi

  # Get selected instance
  selected_instance="${INSTANCES[$((selection - 1))]}"
  NAMESPACE=$(echo "$selected_instance" | awk '{print $1}')
  CLUSTER=$(echo "$selected_instance" | awk '{print $2}')

  readonly NAMESPACE
  readonly CLUSTER

  echo ""
  echo "✓ Selected: Namespace=$NAMESPACE, Cluster=$CLUSTER"
  echo ""
fi

# Validate namespace and cluster are now set (either from auto-discovery or command line)
if [[ -z $CLUSTER ]]; then
  echo "Cluster was not specified. Use --cluster option to specify it."
  error "$USAGE"
fi

if [[ -z $NAMESPACE ]]; then
  echo "Namespace was not specified. Use --namespace option to specify it."
  error "$USAGE"
fi

if [[ $($KUBE_CLIENT get ns "$NAMESPACE" &>/dev/null) == "1" ]]; then
  error "Namespace $NAMESPACE not found! Exiting"
fi

# Check if cluster exists (try both CRD types)
CLUSTER_EXISTS=false
if [[ -n $($KUBE_CLIENT get activemqartemises.broker.amq.io "$CLUSTER" -o name -n "$NAMESPACE" --ignore-not-found 2>/dev/null) ]]; then
  CLUSTER_EXISTS=true
  CLUSTER_TYPE="activemqartemis"
elif [[ -n $($KUBE_CLIENT get brokers.broker.arkmq.org "$CLUSTER" -o name -n "$NAMESPACE" --ignore-not-found 2>/dev/null) ]]; then
  CLUSTER_EXISTS=true
  CLUSTER_TYPE="broker"
fi

if [[ $CLUSTER_EXISTS == false ]]; then
  error "AMQ Broker cluster $CLUSTER in namespace $NAMESPACE not found! Exiting"
fi

RESOURCES=(
  "deployments"
  "statefulsets"
  "replicasets"
  "configmaps"
  "secrets"
  "services"
  "poddisruptionbudgets"
  "roles"
  "rolebindings"
  "networkpolicies"
  "pods"
  "persistentvolumeclaims"
  "ingresses"
  "routes"
)
readonly CLUSTER_RESOURCES=(
  "clusterroles"
  "clusterrolebindings"
)

if [[ "$SECRETS_OPT" == "off" ]]; then
  RESOURCES=("${RESOURCES[@]/secrets}") && readonly RESOURCES
fi

echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "Starting resource collection for AMQ Broker cluster"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "  Namespace: $NAMESPACE"
echo "  Cluster: $CLUSTER"
echo "  Secrets: $SECRETS_OPT"
echo "  Output: $OUT_DIR"
echo ""
echo "Collecting namespace-scoped resources..."
echo ""

get_masked_secrets() {
  echo "secrets"
  mkdir -p "$OUT_DIR"/reports/secrets
  # Try both label types
  local resources
  resources=$($KUBE_CLIENT get secrets -l ActiveMQArtemis="$CLUSTER" -o name -n "$NAMESPACE" 2>/dev/null)
  if [[ -z $resources ]]; then
    resources=$($KUBE_CLIENT get secrets -l Broker="$CLUSTER" -o name -n "$NAMESPACE" 2>/dev/null)
  fi
  for res in $resources; do
    local filename && filename=$(echo "$res" | cut -f 2 -d "/")
    echo "    $res"
    local secret && secret=$($KUBE_CLIENT get "$res" -o yaml -n "$NAMESPACE")
    if [[ "$SECRETS_OPT" == "all" ]]; then
      echo "$secret" > "$OUT_DIR"/reports/secrets/"$filename".yaml
    else
      echo "$secret" | sed "$SE" > "$OUT_DIR"/reports/secrets/"$filename".yaml
    fi
  done
}

get_namespaced_yamls() {
  local type="$1"
  mkdir -p "$OUT_DIR"/reports/"$type"
  local resources
  # Try both label types
  resources=$($KUBE_CLIENT get "$type" -l ActiveMQArtemis="$CLUSTER" -o name -n "$NAMESPACE" 2>/dev/null ||true)
  if [[ -z $resources ]]; then
    resources=$($KUBE_CLIENT get "$type" -l Broker="$CLUSTER" -o name -n "$NAMESPACE" 2>/dev/null ||true)
  fi
  echo "$type"
  if [[ -n $resources ]]; then
    for res in $resources; do
      local filename && filename=$(echo "$res" | cut -f 2 -d "/")
      echo "    $res"
      if [[ "$SECRETS_OPT" == "all" ]]; then
        $KUBE_CLIENT get "$res" -o yaml -n "$NAMESPACE" > "$OUT_DIR"/reports/"$type"/"$filename".yaml
      else
        $KUBE_CLIENT get "$res" -o yaml -n "$NAMESPACE" | sed "$SE" > "$OUT_DIR"/reports/"$type"/"$filename".yaml
      fi
    done
  fi
}

for RES in "${RESOURCES[@]}"; do
  if [[ -n "$RES" ]]; then
    get_namespaced_yamls "$RES"
  fi
done

get_nonnamespaced_yamls() {
  local type="$1"
  mkdir -p "$OUT_DIR"/reports/"$type"
  local resources
  local error_output
  # Try both label types
  error_output=$($KUBE_CLIENT get "$type" -l ActiveMQArtemis=$CLUSTER -o name 2>&1)
  local exit_code=$?

  # If first label didn't work, try the second
  if [[ $exit_code -ne 0 ]] || [[ -z $error_output ]]; then
    error_output=$($KUBE_CLIENT get "$type" -l Broker=$CLUSTER -o name 2>&1)
    exit_code=$?
  fi

  if [[ $exit_code -ne 0 ]]; then
    if [[ "$error_output" == *"Forbidden"* ]] || [[ "$error_output" == *"forbidden"* ]]; then
      echo "$type (⚠️  skipped - insufficient cluster permissions)"
      return 0
    else
      echo "$type (⚠️  skipped - not accessible)"
      return 0
    fi
  fi

  resources=$(echo "$error_output")
  echo "$type"

  if [[ -n $resources ]]; then
    for res in $resources; do
      echo "    $res"
      res=$(echo "$res" | cut -d "/" -f 2)
      $KUBE_CLIENT get "$type" "$res" -o yaml 2>/dev/null | sed "$SE" > "$OUT_DIR"/reports/"$type"/"$res".yaml || echo "        (failed to retrieve)"
    done
  fi
}

echo ""
echo "Collecting cluster-scoped resources (may require cluster admin rights)..."
for RES in "${CLUSTER_RESOURCES[@]}"; do
  get_nonnamespaced_yamls "$RES"
done
echo ""

get_pod_logs() {
  local pod="$1"
  local con="${2-}"
  if [[ -n $pod ]]; then
    local names && names=$($KUBE_CLIENT -n "$NAMESPACE" get po "$pod" -o jsonpath='{.spec.containers[*].name}' --ignore-not-found)
    local count && count=$(echo "$names" | wc -w)
    local logs
    mkdir -p "$OUT_DIR"/reports/podlogs
    if [[ "$count" -eq 1 ]]; then
      logs="$($KUBE_CLIENT -n "$NAMESPACE" logs "$pod" ||true)"
      if [[ -n $logs ]]; then printf "%s" "$logs" > "$OUT_DIR"/reports/podlogs/"$pod".log; fi
      logs="$($KUBE_CLIENT -n "$NAMESPACE" logs "$pod" -p 2>/dev/null ||true)"
      if [[ -n $logs ]]; then printf "%s" "$logs" > "$OUT_DIR"/reports/podlogs/"$pod".log.0; fi
      # shellcheck disable=SC2016
      # append thread dump to logs (no external dependency needed)
      $KUBE_CLIENT -n "$NAMESPACE" exec -i "$pod" -- sh -c 'kill -QUIT 1' 2>/dev/null ||true
    fi
    if [[ "$count" -gt 1 && -n "$con" && "$names" == *"$con"* ]]; then
      logs="$($KUBE_CLIENT -n "$NAMESPACE" logs "$pod" -c "$con" ||true)"
      if [[ -n $logs ]]; then printf "%s" "$logs" > "$OUT_DIR"/reports/podlogs/"$pod"-"$con".log; fi
      logs="$($KUBE_CLIENT -n "$NAMESPACE" logs "$pod" -p -c "$con" 2>/dev/null ||true)"
      if [[ -n $logs ]]; then printf "%s" "$logs" > "$OUT_DIR"/reports/podlogs/"$pod"-"$con".log.0; fi
      # shellcheck disable=SC2016
      # append thread dump to logs (no external dependency needed)
      $KUBE_CLIENT -n "$NAMESPACE" exec -i "$pod" -c "$con" -- sh -c 'kill -QUIT 1' 2>/dev/null ||true
    fi
  fi
}

echo "clusteroperator"
CO_DEPLOY=$($KUBE_CLIENT get deploy amq-broker-controller-manager -o name -n "$NAMESPACE" --ignore-not-found) && readonly CO_DEPLOY
if [[ -n $CO_DEPLOY ]]; then
  echo "$CO_DEPLOY"
  $KUBE_CLIENT get deploy amq-broker-controller-manager -o yaml -n "$NAMESPACE" > "$OUT_DIR"/reports/deployments/cluster-operator.yaml
  $KUBE_CLIENT get po -l name=amq-broker-operator -o yaml -n "$NAMESPACE" > "$OUT_DIR"/reports/pods/cluster-operator.yaml
  CO_POD=$($KUBE_CLIENT get po -l name=amq-broker-operator -o name -n "$NAMESPACE" --ignore-not-found)
  if [[ -n $CO_POD ]]; then
    echo "    $CO_POD"
    CO_POD=$(echo "$CO_POD" | cut -d "/" -f 2) && readonly CO_POD
    get_pod_logs "$CO_POD"
  fi
else
  $KUBE_CLIENT get deploy amq-broker-controller-manager -o yaml -n "openshift-operators" > "$OUT_DIR"/reports/deployments/cluster-operator.yaml
  $KUBE_CLIENT get po -l name=amq-broker-operator -o yaml -n "openshift-operators" > "$OUT_DIR"/reports/pods/cluster-operator.yaml
  CO_POD=$($KUBE_CLIENT get po -l name=amq-broker-operator -o name -n "openshift-operators" --ignore-not-found)
  if [[ -n $CO_POD ]]; then
    echo "    $CO_POD"
    #get_pod_logs "$CO_POD"
    mkdir -p "$OUT_DIR"/reports/podlogs
    $KUBE_CLIENT -n "openshift-operators" logs "$CO_POD" > "$OUT_DIR"/reports/podlogs/amq-broker-controller-manager.log
  fi
fi

CO_RS=$($KUBE_CLIENT get rs -l name=amq-broker-operator -o name -n "$NAMESPACE" --ignore-not-found)
if [[ -n $CO_RS ]]; then
  echo "    $CO_RS"
  CO_RS=$(echo "$CO_RS" | tail -n1) && echo "    $CO_RS"
  CO_RS=$(echo "$CO_RS" | cut -d "/" -f 2) && readonly CO_RS
  $KUBE_CLIENT get rs "$CO_RS" -n "$NAMESPACE" > "$OUT_DIR"/reports/replicasets/"$CO_RS".yaml
fi

echo "customresources"
mkdir -p "$OUT_DIR"/reports/crds "$OUT_DIR"/reports/crs
# Collect all AMQ Broker CRDs (works with both rhel8 and rhel9 operators)
# Using explicit CRD names to avoid grep alias issues on macOS
AMQ_CRDS=(
  "activemqartemises.broker.amq.io"
  "activemqartemisaddresses.broker.amq.io"
  "activemqartemisscaledowns.broker.amq.io"
  "activemqartemissecurities.broker.amq.io"
  "brokers.broker.arkmq.org"
  "brokerapps.broker.arkmq.org"
  "brokerservices.broker.arkmq.org"
)
CRDS=""
for crd_name in "${AMQ_CRDS[@]}"; do
  if $KUBE_CLIENT get crd "$crd_name" &>/dev/null; then
    CRDS="$CRDS $crd_name"
  fi
done
readonly CRDS
for CRD in $CRDS; do
  RES=$($KUBE_CLIENT get "$CRD" -o name -n "$NAMESPACE" --ignore-not-found 2>/dev/null | cut -d "/" -f 2)
  if [[ -n $RES ]]; then
    echo "    $CRD"
    $KUBE_CLIENT get crd "$CRD" -o yaml > "$OUT_DIR"/reports/crds/"$CRD".yaml 2>/dev/null || true
    for j in $RES; do
      RES=$(echo "$j" | cut -f 1 -d " ")
      $KUBE_CLIENT get "$CRD" "$RES" -n "$NAMESPACE" -o yaml > "$OUT_DIR"/reports/crs/"$CRD"-"$RES".yaml 2>/dev/null || true
      echo "        $RES"
    done
  fi
done

# Collect extraMounts ConfigMaps and Secrets from ActiveMQArtemis/Broker CRs
echo "extramounts"
mkdir -p "$OUT_DIR"/reports/extramounts/configmaps "$OUT_DIR"/reports/extramounts/secrets

# Try ActiveMQArtemis CR first
ARTEMIS_CR=$($KUBE_CLIENT get activemqartemises.broker.amq.io "$CLUSTER" -n "$NAMESPACE" -o jsonpath='{.spec.deploymentPlan.extraMounts}' 2>/dev/null)
EXTRA_CONFIGMAPS=""
EXTRA_SECRETS=""

if [[ -n $ARTEMIS_CR ]]; then
  # Extract ConfigMaps from extraMounts using jsonpath
  EXTRA_CONFIGMAPS=$($KUBE_CLIENT get activemqartemises.broker.amq.io "$CLUSTER" -n "$NAMESPACE" -o jsonpath='{.spec.deploymentPlan.extraMounts.configMaps[*]}' 2>/dev/null || true)
  # Extract Secrets from extraMounts using jsonpath
  EXTRA_SECRETS=$($KUBE_CLIENT get activemqartemises.broker.amq.io "$CLUSTER" -n "$NAMESPACE" -o jsonpath='{.spec.deploymentPlan.extraMounts.secrets[*]}' 2>/dev/null || true)
else
  # Try Broker CR
  BROKER_CR=$($KUBE_CLIENT get brokers.broker.arkmq.org "$CLUSTER" -n "$NAMESPACE" -o jsonpath='{.spec.deploymentPlan.extraMounts}' 2>/dev/null)
  if [[ -n $BROKER_CR ]]; then
    # Extract ConfigMaps from extraMounts using jsonpath
    EXTRA_CONFIGMAPS=$($KUBE_CLIENT get brokers.broker.arkmq.org "$CLUSTER" -n "$NAMESPACE" -o jsonpath='{.spec.deploymentPlan.extraMounts.configMaps[*]}' 2>/dev/null || true)
    # Extract Secrets from extraMounts using jsonpath
    EXTRA_SECRETS=$($KUBE_CLIENT get brokers.broker.arkmq.org "$CLUSTER" -n "$NAMESPACE" -o jsonpath='{.spec.deploymentPlan.extraMounts.secrets[*]}' 2>/dev/null || true)
  fi
fi

if [[ -n $EXTRA_CONFIGMAPS ]]; then
  echo "    configmaps from extraMounts:"
  for cm in $EXTRA_CONFIGMAPS; do
    echo "        $cm"
    $KUBE_CLIENT get configmap "$cm" -n "$NAMESPACE" -o yaml > "$OUT_DIR"/reports/extramounts/configmaps/"$cm".yaml 2>/dev/null || echo "        WARNING: ConfigMap $cm not found"
  done
fi

if [[ -n $EXTRA_SECRETS ]]; then
  echo "    secrets from extraMounts:"
  for secret in $EXTRA_SECRETS; do
    echo "        $secret"
    if [[ "$SECRETS_OPT" == "all" ]]; then
      $KUBE_CLIENT get secret "$secret" -n "$NAMESPACE" -o yaml > "$OUT_DIR"/reports/extramounts/secrets/"$secret".yaml 2>/dev/null || echo "        WARNING: Secret $secret not found"
    elif [[ "$SECRETS_OPT" == "hidden" ]]; then
      $KUBE_CLIENT get secret "$secret" -n "$NAMESPACE" -o yaml 2>/dev/null | sed "$SE" > "$OUT_DIR"/reports/extramounts/secrets/"$secret".yaml 2>/dev/null || echo "        WARNING: Secret $secret not found"
    fi
  done
fi

echo "events"
EVENTS=$($KUBE_CLIENT get event -n "$NAMESPACE" --ignore-not-found) && readonly EVENTS
if [[ -n $EVENTS ]]; then
  mkdir -p "$OUT_DIR"/reports/events
  echo "$EVENTS" > "$OUT_DIR"/reports/events/events.txt
fi

echo "podlogs"
mkdir -p "$OUT_DIR"/reports/configs
# Try both label types
PODS=$($KUBE_CLIENT get po -l ActiveMQArtemis="$CLUSTER" -o name -n "$NAMESPACE" 2>/dev/null | cut -d "/" -f 2)
if [[ -z $PODS ]]; then
  PODS=$($KUBE_CLIENT get po -l Broker="$CLUSTER" -o name -n "$NAMESPACE" 2>/dev/null | cut -d "/" -f 2)
fi
readonly PODS
for POD in $PODS; do
  echo "    $POD"
  get_pod_logs "$POD" $CLUSTER-container 
  get_pod_logs "$POD" $CLUSTER-container-init
  files=(
    artemis-roles.properties
    artemis-users.properties
    artemis.profile
    bootstrap.xml
    broker.xml
    jgroups-ping.xml
    jolokia-access.xml
    log4j2.properties
    login.config
    management.xml
)

for file in "${files[@]}"; do
    echo "Processing file: $file"
    $KUBE_CLIENT exec -i "$POD" -n "$NAMESPACE" -c "$CLUSTER-container" -- \
        cat "/home/jboss/amq-broker/etc/$file" > "$OUT_DIR/reports/configs/$file" \
        2>/dev/null || true
done

# PostConfig.sh -- init

    echo "Processing file: post-config.sh"
    $KUBE_CLIENT exec -i "$POD" -n "$NAMESPACE" -c "$CLUSTER-container" -- \
        cat "/amq/scripts/post-config.sh" > "$OUT_DIR/reports/configs/post-config.sh" \
        2>/dev/null || true
done

FILENAME="report-$(date +"%d-%m-%Y_%H-%M-%S")"
OLD_DIR="$(pwd)"
cd "$OUT_DIR" || exit
zip -qr "$FILENAME".zip ./reports/
cd "$OLD_DIR" || exit
if [[ $OUT_DIR == *"tmp."* ]]; then
  # let's keep the old behavior when --out-dir is not specified
  mv "$OUT_DIR"/"$FILENAME".zip ./
fi

echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "✓ Report successfully created: $FILENAME.zip"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo ""
echo "Summary:"
echo "  Namespace: $NAMESPACE"
echo "  Cluster: $CLUSTER"
echo "  Output: $FILENAME.zip"
echo ""

# Check if cluster-scoped resources directory exists and is empty
if [[ -d "$OUT_DIR/reports/clusterroles" ]] && [[ -z "$(ls -A "$OUT_DIR/reports/clusterroles" 2>/dev/null)" ]] && \
   [[ -d "$OUT_DIR/reports/clusterrolebindings" ]] && [[ -z "$(ls -A "$OUT_DIR/reports/clusterrolebindings" 2>/dev/null)" ]]; then
  echo "Note: Cluster-scoped resources (clusterroles, clusterrolebindings) were skipped"
  echo "      due to insufficient cluster permissions. This is normal for non-admin users."
  echo ""
fi

echo "Report collected successfully!"
echo ""
} # this ensures the entire script is downloaded #
