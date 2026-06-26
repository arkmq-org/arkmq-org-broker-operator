#!/bin/bash

# service accounts, role/rolebinding, operator resources
mkdir -p $1
# crds
mkdir -p $1/crds

destdir=$1
crdsdir=$1/crds
file=()
resource_kind=""
resource_name=""

SINGLE_INSTALL_YML="${destdir}/arkmq-org-broker-operator.yaml"

OPERATOR_NAMESPACE=${2}
echo "OPERATOR_NAMESPACE:${OPERATOR_NAMESPACE}"

# write a single yaml.
# it takes one parameter:
# 1 array that contains total lines of the yaml
function writeFile() {

  true_kind=$resource_kind
  case $resource_kind in

    CustomResourceDefinition)
      if [[ ${resource_name} =~ (activemqartemises) ]]; then
        createFile "$crdsdir/broker_activemqartemis_crd.yaml"
      elif [[ ${resource_name} =~ (activemqartemissecurities) ]]; then
        createFile "$crdsdir/broker_activemqartemissecurity_crd.yaml"
      elif [[ ${resource_name} =~ (activemqartemisaddresses) ]]; then
        createFile "$crdsdir/broker_activemqartemisaddress_crd.yaml"
      elif [[ ${resource_name} =~ (activemqartemisscaledowns) ]]; then
        createFile "$crdsdir/broker_activemqartemisscaledown_crd.yaml"
      elif [[ ${resource_name} =~ (brokerclusters\.) ]]; then
        createFile "$crdsdir/broker_brokercluster_crd.yaml"
      elif [[ ${resource_name} =~ (brokers\.) ]]; then
        createFile "$crdsdir/broker_broker_crd.yaml"
      elif [[ ${resource_name} =~ (brokerservices) ]]; then
        createFile "$crdsdir/broker_brokerservice_crd.yaml"
      elif [[ ${resource_name} =~ (brokerapps) ]]; then
        createFile "$crdsdir/broker_brokerapp_crd.yaml"
      else
        createFile "$crdsdir/${resource_name}.yaml"
      fi
      ;;

    Deployment)
      createFile "$destdir/operator.yaml"
      ;;

    Role)
      if [[ ${resource_name} =~ (operator) ]]; then
        createFile "$destdir/role.yaml"
        true_kind="ClusterRole"
        createFile "$destdir/cluster_role.yaml"
        sed -i.bak 's/kind: Role/kind: ClusterRole/' \
          "$destdir/cluster_role.yaml" && rm "$destdir/cluster_role.yaml.bak"
      elif [[ ${resource_name} =~ (leader-election) ]]; then
        createFile "$destdir/election_role.yaml"
      else
        createFile "$crdsdir/${resource_name}.yaml"
      fi
      ;;

    RoleBinding)
      if [[ ${resource_name} =~ (operator) ]]; then
        createFile "$destdir/role_binding.yaml"
        true_kind="ClusterRoleBinding"
        createFile "$destdir/cluster_role_binding.yaml"
        sed -i.bak -e 's/kind: Role/kind: ClusterRole/' \
          -e 's/kind: RoleBinding/kind: ClusterRoleBinding/' \
          "$destdir/cluster_role_binding.yaml" && rm "$destdir/cluster_role_binding.yaml.bak"
        echo "  namespace: ${OPERATOR_NAMESPACE}" >> \
          "$destdir/cluster_role_binding.yaml"
      elif [[ ${resource_name} =~ (leader-election) ]]; then
        createFile "$destdir/election_role_binding.yaml"
      else
        createFile "$crdsdir/${resource_name}.yaml"
      fi
      ;;

    ServiceAccount)
      createFile "$destdir/service_account.yaml"
      ;;

    NetworkPolicy)
      createFile "$destdir/network_policy.yaml"
      ;;

    Namespace)
      echo "Skipping ${resource_kind}:${resource_name}"
      makeSingleInstall
      ;;
    
    *)
      createFile "$destdir/${resource_kind}_${resource_name}.yaml"
      ;;
    esac
}

function createFile() {
  echo "Writing $1"
  rm -rf $1
  for value in "${file[@]}"; do
    printf "%s\n" "${value}" >> $1
  done
  makeSingleInstall
}

function beginFile() {
  if [[ ${#file[@]} > 0 ]]
  then
    # it gathered a single file now
    writeFile file
  fi
  file=()
}

function appendFile() {
  file+=("$1")
}

function makeSingleInstall() {
  if [[ "${true_kind}" == "ClusterRole" && ${resource_name} =~ (operator) ]]; then
    for val in "${singleYaml[@]}"; do
      if [[ "${val}" == "kind: Role" ]]; then
        val="kind: ClusterRole"
      fi
      printf "%s\n" "${val}" >> ${SINGLE_INSTALL_YML}
    done
    singleYaml=()
  elif [[ "${true_kind}" == "ClusterRoleBinding" && ${resource_name} =~ (operator) ]]; then
    for val in "${singleYaml[@]}"; do
      if [[ "${val}" == "kind: RoleBinding" ]]; then
        val="kind: ClusterRoleBinding"
      elif [[ "${val}" == "  kind: Role" ]]; then
        val="  kind: ClusterRole"
      fi
      printf "%s\n" "${val}" >> ${SINGLE_INSTALL_YML}
    done
    singleYaml=()
  elif [[ "${true_kind}" == "Role" && ${resource_name} =~ (operator) ]]; then
    echo "ignore role"
  elif [[ "${true_kind}" == "RoleBinding" && ${resource_name} =~ (operator) ]]; then
    echo "ignore rolebinding"
  elif [[ "${true_kind}" == "Deployment" ]]; then
    found_watch_ns="false"
    for val in "${singleYaml[@]}"; do
      if [[ "${val}" == "        - name: WATCH_NAMESPACE" ]]; then
        found_watch_ns="true"
        printf "%s\n" "${val}" >> ${SINGLE_INSTALL_YML}
      elif [[ "${found_watch_ns}" == "true" ]]; then
        if [[ "${val}" == "          valueFrom:" ]]; then
          val="          value: ''"
          printf "%s\n" "${val}" >> ${SINGLE_INSTALL_YML}
        elif [[ "${val}" == "              fieldPath: metadata.namespace" ]]; then
          found_watch_ns=false
        fi
      else
        printf "%s\n" "${val}" >> ${SINGLE_INSTALL_YML}
      fi
    done
    singleYaml=()
  else
    for val in "${singleYaml[@]}"; do
      printf "%s\n" "${val}" >> ${SINGLE_INSTALL_YML}
    done
    singleYaml=()
  fi
}

IFS=''
rm -rf ${SINGLE_INSTALL_YML}
singleYaml=()
while read -r line
do
  singleYaml+=("$line")
  if [[ $line =~ ^---$ ]]
  then
    beginFile
    resource_kind=""
    resource_name=""
    meta_start="false"
  else
    to_append="true"

    if [[ $resource_kind == "" && $line =~ ^(kind: ) ]]
    then
      resource_kind=$(echo $line | cut -b 7-)
    fi

    if [[ $line =~ ^(metadata:) ]]
    then
      meta_start="true"
    elif [[ ! $line =~ ^("  ") ]]; then
      meta_start="false"
    elif [[ ${meta_start} == "true" ]]; then
      if [[ $line =~ ^(  name: ) ]]; then
        resource_name=$(echo $line | cut -b 9-)
      elif [[ $line =~ ^(  namespace: ) ]]; then
        to_append="false"
      fi
    fi

    if [[ $resource_kind == "RoleBinding" ]]; then
      if [[ $line =~ ^(subjects:) ]]; then
        rb_subjects="true"
      elif [[ ${rb_subjects} == "true" ]]; then
        if [[ $line =~ ^(  namespace: ) ]]; then
          to_append="false"
        fi
      fi
    fi

    if [[ $to_append == "true" ]]; then
      appendFile "$line"
    fi
  fi
done
# check the last one
beginFile

