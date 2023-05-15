#!/usr/bin/env bash
set -eu

cd "$(dirname "${BASH_SOURCE[0]}")"/../../.. || exit 1

download_artifacts() {
  local src
  local target
  src=$1
  dest=$2
  mkdir -p "${dest}"
  buildkite-agent artifact download "${src}/sourcegraph-backend-*" "${dest}"
}

VERSION=$(./enterprise/dev/app/app_version.sh)

download_artifacts "${RELEASE_DIR:-".bin"}" dist/

gh release create -d -p ${VERSION} --notes "generated release from buildkite" ./dist/*
