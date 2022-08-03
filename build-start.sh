#!/bin/bash

go mod tidy

go build -o cronjob-sidecar-watcher main.go
./cronjob-sidecar-watcher
