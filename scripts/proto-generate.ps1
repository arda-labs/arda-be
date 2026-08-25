$ErrorActionPreference = "Stop"

$root = Split-Path -Parent $PSScriptRoot
$out = Join-Path $root "libs/go/arda-proto"
$protoRoot = Join-Path $root "proto"
$protoFiles = Get-ChildItem -Path $protoRoot -Recurse -Filter *.proto |
  Sort-Object FullName |
  ForEach-Object { $_.FullName }

if (-not $protoFiles) {
  throw "No .proto sources found under $protoRoot"
}

& protoc `
  -I $protoRoot `
  --go_out=$out `
  --go_opt=module=github.com/arda-labs/arda/libs/go/arda-proto `
  --go-grpc_out=$out `
  --go-grpc_opt=module=github.com/arda-labs/arda/libs/go/arda-proto `
  $protoFiles

if ($LASTEXITCODE -ne 0) {
  throw "protoc failed with exit code $LASTEXITCODE"
}

Write-Host "Generated $($protoFiles.Count) protobuf contracts in $out"

