#!/usr/bin/env bash

set -eu -o pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/../../../../.."
# cd /root # TODO: Dev only

GCP_PROJECT="sourcegraph-ci"
GCS_BUCKET="package-repository"
ARCH="x86_64"
branch="main"

cd wolfi-packages/packages/$ARCH

# Use GCP tooling to upload new package to repo, ensuring it's on the right branch
# Check that this exact package does not already exist in the repo - fail if so

# TODO: Support branches for uploading
# TODO: Check for existing files only if we're on main - overwriting is permitted on branches

# TODO: Remove, do in https://github.com/sourcegraph/infrastructure/blob/main/docker-images/buildkite-agent-stateless-bazel/pre-entrypoint.sh#L11-L16
# TODO: (or somewhere better)
echo " * Attempting to activate Gcloud Auth service account"
gcloud auth activate-service-account --key-file "/mnt/gcloud-service-account/gcloud-service-account.json" --project "$GCP_PROJECT"

echo " * Uploading package to repository"

# List all .apk files under wolfi-packages/packages/$ARCH/
apks=(*.apk)
for apk in "${apks[@]}"; do
  echo " * Processing $apk"
  dest_path="gs://$GCS_BUCKET/packages/$branch/$ARCH/"
  dest_path="gs://$GCS_BUCKET/test.txt"
  echo "   -> File path: $dest_path / $apk"

  # Generate index fragment for this package
  melange index -o "$apk.APKINDEX.tar.gz" "$apk"
  tar zxf "$apk.APKINDEX.tar.gz"
  index_fragment="$apk.APKINDEX.fragment"
  mv APKINDEX "$index_fragment"
  echo "   * Generated index fragment '$index_fragment"

  # Check if this version of the package already exists in bucket
  echo "   * Checking if this package version already exists in repo..."
  if gsutil -q -u "$GCP_PROJECT" stat "$dest_path/$apk"; then
    echo "$apk: A package with this version already exists, and cannot be overwritten."
    echo "Resolve this issue by incrementing the \`epoch\` field in the package's YAML file."
    # exit 1
  else
    echo "   * File does not exist, uploading..."
  fi

  echo "   * Uploading package and index fragment to repo"
  gsutil -u "$GCP_PROJECT" cp -n "$apk" "$index_fragment" "$dest_path"
done
