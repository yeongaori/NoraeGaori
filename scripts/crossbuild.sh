#!/bin/sh
set -e

version="${VERSION:-dev}"
dist="${DIST:-dist}"
package="$dist/.package"

mkdir -p "$dist"

build() {
    goos="$1" goarch="$2" name="$3"
    binary="noraegaori"
    extension="tar.gz"
    if [ "$goos" = "windows" ]; then
        binary="noraegaori.exe"
        extension="zip"
    fi

    echo "Building $name..."
    directory="noraegaori-${version}-${name}"
    staging="$package/$directory"
    rm -rf "$staging"
    mkdir -p "$staging/config"
    GOOS="$goos" GOARCH="$goarch" CGO_ENABLED=0 go build -ldflags="-s -w" -o "$staging/$binary" .
    cp config/*.example.json "$staging/config/"
    cp .env.example README.md LICENSE "$staging/"

    archive="$(pwd)/$dist/${directory}.${extension}"
    rm -f "$archive"
    if [ "$extension" = "zip" ]; then
        (cd "$package" && zip -rq "$archive" "$directory")
    else
        tar czf "$archive" -C "$package" "$directory"
    fi
    (cd "$dist" && sha256sum "${directory}.${extension}" > "${directory}.${extension}.sha256")
    rm -rf "$staging"
}

build linux amd64 linux-x64
build linux 386 linux-x86
build linux arm64 linux-arm64
build windows amd64 windows-x64
build windows 386 windows-x86
build windows arm64 windows-arm64

rm -rf "$package"
