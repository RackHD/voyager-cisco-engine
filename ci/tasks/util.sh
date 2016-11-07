#!/bin/bash
set -e -x

check_param() {
  local name=$1
  local value=$(eval echo '$'$name)
  if [ "$value" == 'replace-me' ]; then
    echo "environment variable $name must be set"
    exit 1
  fi
}

set_env() {
    WORK_DIR=${PWD}
    export GOPATH=${PWD}
    SOURCE_DIR=${GOPATH}/src/github.com/RackHD
    mkdir -p ${SOURCE_DIR}
}

build_binary() {
    cp -r $1 ${SOURCE_DIR}/$1
    pushd ${SOURCE_DIR}/$1
      make deps
      make build
      cp -r bin ${WORK_DIR}/build
      cp -r templates ${WORK_DIR}/build
      cp -r workflows ${WORK_DIR}/build
      cp Dockerfile ${WORK_DIR}/build
    popd
}
