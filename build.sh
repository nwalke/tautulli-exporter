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

available_goarchs=$(go tool dist list | tr '\n' ',')
available_dockerarchs=$(docker buildx inspect | grep Platforms)

platforms=("darwin" "windows" "netbsd" "openbsd" "linux" "freebsd" "plan9")
archs=("amd64" "arm64")
docker_archs=""

for p in "${platforms[@]}"; do
  for a in "${archs[@]}"; do
    output_name=$package_name'-'$p'-'$a

    if [ $a = "windows" ]; then
        output_name+='.exe'
    fi

    if [ $(echo ${available_goarchs} | grep -c "$p/$a") -ge 1 ]
    then
      echo "Building for $p/$a"
      GOOS=$p CGO_ENABLED=0 GOARCH=$a go build -a -installsuffix cgo -ldflags "-w -s -X main.version=$version" -o .build/$output_name

      if [ $(echo ${available_dockerarchs}| grep -c "$p/$a") -ge 1 ]
      then
        docker_archs+="$p/$a,"
      fi
    else
      echo "Can't build for $p/$a"
    fi
  done
done

echo "Building docker image for this platforms: ${docker_archs%,}"
docker buildx build --platform=${docker_archs%,} -t $full_package_name -t $full_package_name:$version --push .

exit

git tag -a $version -m "$version"
git push origin tag $version
