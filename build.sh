#!/usr/bin/env sh

# Get the current version from git
version=$(git describe --tags --always)
echo "Building HubToTea version $version"

mkdir -p build

# Build the application with the current version
cd hubtotea && go build \
  -ldflags "-X main.Version=$version" \
  -o ../build/hubtotea
  "$@"