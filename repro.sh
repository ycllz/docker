#!/bin/bash
set -e

pkg_list=$(go list -e \
	-f '{{if ne .Name "github.com/docker/docker"}}
		{{.ImportPath}}
	    {{end}}' \
	./...)

go test github.com/docker/docker/api
