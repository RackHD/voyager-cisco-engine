#!/bin/bash

set -e -x
source voyager-cisco-engine/ci/tasks/util.sh

check_param GITHUB_USER
check_param GITHUB_PASSWORD

echo -e "machine github.com\n  login $GITHUB_USER\n  password $GITHUB_PASSWORD" >> ~/.netrc

set_env
build_binary "voyager-cisco-engine"
