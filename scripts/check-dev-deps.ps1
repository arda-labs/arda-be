param(
  [string]$Node = "192.168.100.201",
  [int]$PostgresPort = 30432,
  [int]$ValkeyPort = 30379,
  [int]$NatsPort = 4222,
  [int]$HydraAdminPort = 30445,
  [int]$KratosAdminPort = 30446,
  [switch]$RequireNats
)

$ErrorActionPreference = "Stop"

function Test-Tcp($Name, $HostName, $Port, [switch]$Required = $true) {
  $ok = Test-NetConnection -ComputerName $HostName -Port $Port -InformationLevel Quiet
  if ($ok) {
    Write-Host "[ok] $Name ${HostName}:$Port"
    return $true
  }
  $level = if ($Required) { "fail" } else { "warn" }
  Write-Host "[$level] $Name ${HostName}:$Port not reachable"
  if ($Required) {
    return $false
  }
  return $true
}

$results = @()
$results += Test-Tcp "PostgreSQL" $Node $PostgresPort
$results += Test-Tcp "Valkey" $Node $ValkeyPort
$results += Test-Tcp "Hydra admin" $Node $HydraAdminPort
$results += Test-Tcp "Kratos admin" $Node $KratosAdminPort
$results += Test-Tcp "NATS local/forwarded" "127.0.0.1" $NatsPort -Required:$RequireNats

if ($results -contains $false) {
  Write-Host "One or more required dev dependencies are unavailable."
  exit 1
}

Write-Host "Dev dependency check completed."
