#!/usr/bin/env bash

# QYT App

## The functions in this file can be invoked like:
### ./README run

webappPackage="../qyt-webapp"
guiAppPackage="."
assetsDir="${guiAppPackage}/embed/assets"

## Setup Dependencies
function init() {
    go mod download
    mkdir -p "${assetsDir}"
    cp "$(go env GOROOT)/misc/wasm/wasm_exec.js" "${assetsDir}"
}

## Build Webapp Assets
function build_webapp() {
    rm -rf "${assetsDir}/*.wasm" 
    GOOS=js GOARCH=wasm go build -ldflags="-s -w" -o "${assetsDir}/main.wasm" "${webappPackage}"
}

## Run
function run() {
    build_webapp
    go run ./
}

if [ $# -ne 0 ]; then
    "$1"
fi
