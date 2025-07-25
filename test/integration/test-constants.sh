#!/usr/bin/env bash

# Integration Test Constants for Amazon EKS Hybrid Nodes
# This file centralizes version definitions used across integration tests

# Supported Kubernetes versions for install/uninstall tests
declare SUPPORTED_VERSIONS=(1.28 1.29 1.30 1.31 1.32 1.33)

# Default versions for upgrade tests and single-version tests
declare DEFAULT_INITIAL_VERSION=1.32
declare CURRENT_VERSION=1.33  # Used for both upgrade target version and single-version tests


# Export arrays and variables for use in test scripts
export SUPPORTED_VERSIONS
export DEFAULT_INITIAL_VERSION
export CURRENT_VERSION
