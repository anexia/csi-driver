#!/usr/bin/env bash

echo "==> Checking source code dependencies..."
go mod tidy
git diff --exit-code -- go.mod go.sum || \
   (echo; echo "Found differences in go.mod/go.sum files. Run 'go mod tidy' or revert go.mod/go.sum changes."; exit 1)
# reset go.sum to state before checking if it is clean
git checkout -q go.sum
