#!/usr/bin/bash
# local build
version=$(git describe --tags | head -1) 
go build -v -ldflags="-X 'main.BuildVersion=${version}'" .
