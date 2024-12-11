#!/usr/bin/env bash

mkdir -p build/;
GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o build/convert_flac_to_opus convert_flac_to_opus.go;
GOOS=windows GOARCH=amd64 go build -ldflags="-s -w" -o build/convert_flac_to_opus.exe convert_flac_to_opus.go;