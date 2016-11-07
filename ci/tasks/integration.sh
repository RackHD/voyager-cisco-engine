#!/bin/bash

set -e -x
source voyager-cisco-engine/ci/tasks/util.sh

# Get the env ip from locks input
export INTEGRATION_VM_IP=$(cat $PWD/it-env/metadata)

check_param INTEGRATION_VM_IP
check_param INTEGRATION_VM_SSH_KEY
check_param INTEGRATION_VM_USER

echo "$INTEGRATION_VM_SSH_KEY" > ssh.key
chmod 400 ssh.key

project_path=/home/emc/.gvm/pkgsets/go1.7/global/src/github.com/RackHD
ssh -i ssh.key -o "StrictHostKeyChecking no" ${INTEGRATION_VM_USER}@${INTEGRATION_VM_IP} "rm -rf $project_path && mkdir -p $project_path"
scp -i ssh.key -o "StrictHostKeyChecking no" -r voyager-cisco-engine  ${INTEGRATION_VM_USER}@${INTEGRATION_VM_IP}:$project_path

integration_file=$PWD/voyager-cisco-engine/ci/tasks/run-integration.sh
ssh -i ssh.key -o "StrictHostKeyChecking no" ${INTEGRATION_VM_USER}@${INTEGRATION_VM_IP} 'bash -s' < $integration_file
