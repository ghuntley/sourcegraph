#!/usr/bin/env bash

set -eu

cd "$(dirname "${BASH_SOURCE[0]}")"/../../.. || exit 1

bazelrc() {
  if [[ $(uname -s) == "Darwin" ]]; then
    echo "--bazelrc=.bazelrc --bazelrc=.aspect/bazelrc/ci.macos.bazelrc"
  else
    echo "--bazelrc=.bazelrc --bazelrc=.aspect/bazelrc/ci.bazelrc --bazelrc=.aspect/bazelrc/ci.sourcegraph.bazelrc"
  fi
}

bazel_build() {
  local bazel_cmd
  local platform
  local bin_dir
  bazel_cmd="bazel"
  platform=$1
  bin_dir=$2

  if [[ ${CI:-""} == "true" ]]; then
    bazel_cmd="${bazel_cmd} $(bazelrc)"
  fi

  echo "--- :bazel: Building Sourcegraph Backend (${VERSION}) for platform: ${platform}"
  ${bazel_cmd} build //enterprise/cmd/sourcegraph:sourcegraph --stamp --workspace_status_command=./enterprise/dev/app/app_stamp_vars.sh

  out=$(bazel cquery //enterprise/cmd/sourcegraph:sourcegraph --output=files)
  mkdir -p "${bin_dir}"
  cp -vf "${out}" "${bin_dir}/sourcegraph-backend-${platform}"
}

upload_artifacts() {
  local platform
  local target_path
  platform=$1
  target_path=$2
  buildkite-agent artifact upload "${target_path}"
}


platform() {
  # We need to determine the platform string for the sourcegraph-backend binary
  local arch=""
  local platform=""
  case "$(uname -m)" in
    "amd64")
      arch="x86_64"
      ;;
    "arm64")
      arch="aarch64"
      ;;
    *)
      arch=$(uname -m)
  esac

  case "$(uname -s)" in
    "Darwin")
      platform="${arch}-apple-darwin"
      ;;
    "Linux")
      platform="${arch}-unknown-linux-gnu"
      ;;
    *)
      platform="${arch}-unknown-unknown"
  esac

  echo ${platform}
}

# determine platform if it is not set
PLATFORM=${PLATFORM:-$(platform)}
export PLATFORM

VERSION=$(./enterprise/dev/app/app_version.sh)
export VERSION

bazel_build "${PLATFORM}" ".bin"

if [[ ${CI:-""} == "true" ]]; then
  upload_artifacts "${PLATFORM}" ".bin/sourcegraph-backend-*"
fi
