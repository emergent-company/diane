#!/bin/bash
# One-time setup: git config core.hooksPath .githooks
# Run this after cloning the repo to enable pre-commit checks.
echo "Setting up githooks..."
cd "$(dirname "$0")" || exit 1
git config core.hooksPath .githooks
echo "✅ Pre-commit hooks enabled — will run: gofmt, vet, test, build"
