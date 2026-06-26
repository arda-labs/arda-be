$ErrorActionPreference = "Stop"

$root = Split-Path -Parent $PSScriptRoot
$out = Join-Path $root "libs/go/arda-proto"

protoc `
  -I (Join-Path $root "proto") `
  --go_out=$out `
  --go_opt=module=github.com/arda-labs/arda/libs/go/arda-proto `
  --go-grpc_out=$out `
  --go-grpc_opt=module=github.com/arda-labs/arda/libs/go/arda-proto `
  (Join-Path $root "proto/arda/common/v1/common.proto") `
  (Join-Path $root "proto/arda/platform/v1/platform.proto")

Write-Host "Generated Go protobuf code in $out"

