$ErrorActionPreference = 'Stop'
$root = Split-Path $PSScriptRoot -Parent
$compiler = Get-Command protoc -ErrorAction SilentlyContinue
if ($compiler) { $compilerPath = $compiler.Source }
else {
    $vendored = Get-ChildItem -Path "$env:USERPROFILE/.cargo/registry/src/*/protoc-bin-vendored-win32-*/bin/protoc.exe" -ErrorAction SilentlyContinue | Select-Object -First 1
    if (-not $vendored) { throw 'Install protoc or build services/engine once to provision the vendored compiler.' }
    $compilerPath = $vendored.FullName
}
$env:PATH = "$(go env GOPATH)/bin;$env:PATH"
go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.36.10
if ($LASTEXITCODE -ne 0) { throw 'protoc-gen-go installation failed' }
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.6.0
if ($LASTEXITCODE -ne 0) { throw 'protoc-gen-go-grpc installation failed' }
$output = Join-Path $root 'services/gateway/gen/shieldv1'
New-Item -ItemType Directory -Force -Path $output | Out-Null
& $compilerPath "--proto_path=$root/proto" "--go_out=$output" '--go_opt=paths=source_relative' "--go-grpc_out=$output" '--go-grpc_opt=paths=source_relative' "$root/proto/shield.proto"
if ($LASTEXITCODE -ne 0) { throw 'Protocol generation failed' }
