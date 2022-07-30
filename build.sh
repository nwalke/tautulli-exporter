#!/bin/bash

version="$1"

if [[ -z $version ]]; then
  echo "No version set."
  exit 1
fi

rm .build/*

creator_name="nwalke"
package_name="tautulli_exporter"
full_package_name="$creator_name/$package_name"

platforms=("darwin" "windows" "netbsd" "openbsd" "linux" "freebsd" "plan9")

for i in "${platforms[@]}"; do

  output_name=$package_name'-'$i'-amd64'

  if [ $i = "windows" ]; then
      output_name+='.exe'
  fi

  GOOS=$i CGO_ENABLED=0 GOARCH=amd64 go build -a -installsuffix cgo -ldflags "-w -s -X main.version=$version" -o .build/$output_name
done

docker build -t $full_package_name .
docker tag $full_package_name $full_package_name:$version
docker push $full_package_name
docker push $full_package_name:$version

git tag -a $version -m "$version"
git push origin tag $version
