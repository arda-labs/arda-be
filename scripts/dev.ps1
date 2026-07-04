param(
  [string[]]$Services = @(),
  [string]$Service = "",
  [switch]$List,
  [switch]$Windows,
  [switch]$Tabs
)

$ErrorActionPreference = "Stop"
$Root = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path

$DefaultServices = @(
  "iam-service",
  "platform-service",
  "finance-service",
  "media-service",
  "workflow-service",
  "crm-service",
  "hrm-service",
  "notification-service",
  "mdm-service",
  "auth-gateway"
)

$ServiceEnv = @{
  "iam-service" = @{
    HTTP_ADDR = "0.0.0.0:8080"
  }
  "platform-service" = @{
    HTTP_ADDR = "0.0.0.0:8091"
    GRPC_ADDR = "0.0.0.0:9091"
  }
  "finance-service" = @{
    HTTP_ADDR = "0.0.0.0:8090"
    PLATFORM_GRPC_ADDR = "localhost:9091"
  }
  "media-service" = @{
    HTTP_ADDR = "0.0.0.0:8092"
    GRPC_ADDR = "0.0.0.0:9092"
  }
  "workflow-service" = @{
    HTTP_ADDR = "0.0.0.0:8093"
    GRPC_ADDR = "0.0.0.0:9093"
    CRM_GRPC_ADDR = "localhost:9094"
  }
  "crm-service" = @{
    HTTP_ADDR = "0.0.0.0:8094"
    GRPC_ADDR = "0.0.0.0:9094"
    WORKFLOW_GRPC_ADDR = "localhost:9093"
  }
  "hrm-service" = @{
    HTTP_ADDR = "0.0.0.0:8097"
    WORKFLOW_GRPC_ADDR = "localhost:9093"
  }
  "notification-service" = @{
    HTTP_ADDR = "0.0.0.0:8095"
  }
  "mdm-service" = @{
    HTTP_ADDR = "0.0.0.0:8096"
  }
  "auth-gateway" = @{
    HTTP_ADDR = "0.0.0.0:8082"
    IAM_SERVICE_URL = "http://localhost:8080"
    PLATFORM_SERVICE_URL = "http://localhost:8091"
    FINANCE_SERVICE_URL = "http://localhost:8090"
    MEDIA_SERVICE_URL = "http://localhost:8092"
    WORKFLOW_SERVICE_URL = "http://localhost:8093"
    CRM_SERVICE_URL = "http://localhost:8094"
    HRM_SERVICE_URL = "http://localhost:8097"
    NOTIFICATION_SERVICE_URL = "http://localhost:8095"
    MDM_SERVICE_URL = "http://localhost:8096"
  }
}

if ($List) {
  $DefaultServices
  exit 0
}

function Assert-Service([string]$Name) {
  if (-not $ServiceEnv.ContainsKey($Name)) {
    throw "Unknown service '$Name'. Run scripts/dev.ps1 -List."
  }
  $serviceDir = Join-Path $Root "apps\$Name"
  if (-not (Test-Path $serviceDir)) {
    throw "Service directory not found: $serviceDir"
  }
}

function Start-ServiceWindows([string[]]$Names) {
  $shell = "powershell"
  $pwsh = Get-Command pwsh -ErrorAction SilentlyContinue
  if ($pwsh) {
    $shell = $pwsh.Source
  }

  foreach ($name in $Names) {
    Assert-Service $name
    Start-Process -FilePath $shell -ArgumentList @(
      "-NoExit",
      "-NoProfile",
      "-ExecutionPolicy",
      "Bypass",
      "-File",
      $PSCommandPath,
      "-Service",
      $name
    )
  }
}

function Start-ServiceTabs([string[]]$Names) {
  $wt = Get-Command wt -ErrorAction SilentlyContinue
  if (-not $wt) {
    throw "Windows Terminal CLI 'wt' not found. Use scripts/dev.ps1 for one terminal, or open tabs manually and run scripts/dev.ps1 -Service <name>."
  }

  $args = @("-w", "0")
  $first = $true
  foreach ($name in $Names) {
    Assert-Service $name
    if (-not $first) {
      $args += ";"
    }
    $args += @(
      "new-tab",
      "--title",
      $name,
      "powershell",
      "-NoExit",
      "-NoProfile",
      "-ExecutionPolicy",
      "Bypass",
      "-File",
      $PSCommandPath,
      "-Service",
      $name
    )
    $first = $false
  }

  & $wt.Source @args
}

function Set-ServiceEnvironment([string]$Name) {
  foreach ($entry in $ServiceEnv[$Name].GetEnumerator()) {
    [Environment]::SetEnvironmentVariable($entry.Key, [string]$entry.Value, "Process")
  }
}

function Get-ServiceStamp([string]$ServiceDir) {
  $ignored = @(
    [IO.Path]::DirectorySeparatorChar + ".dev" + [IO.Path]::DirectorySeparatorChar,
    [IO.Path]::DirectorySeparatorChar + "bin" + [IO.Path]::DirectorySeparatorChar,
    [IO.Path]::DirectorySeparatorChar + "tmp" + [IO.Path]::DirectorySeparatorChar
  )
  $files = Get-ChildItem $ServiceDir -Recurse -File -ErrorAction SilentlyContinue |
    Where-Object {
      $path = $_.FullName
      -not ($ignored | Where-Object { $path.Contains($_) }) -and
      ($_.Extension -in @(".go", ".yaml", ".yml", ".sql") -or $_.Name -in @("go.mod", "go.sum"))
    } |
    Sort-Object FullName

  return ($files | ForEach-Object { "$($_.FullName):$($_.LastWriteTimeUtc.Ticks):$($_.Length)" }) -join "|"
}

function Stop-RunningProcess([string]$Name, $Process) {
  if ($null -eq $Process -or $Process.HasExited) {
    return
  }
  Write-Host "[$Name] stopping pid $($Process.Id)"
  Stop-Process -Id $Process.Id -Force -ErrorAction SilentlyContinue
  Wait-Process -Id $Process.Id -Timeout 5 -ErrorAction SilentlyContinue
}

function Build-Service([string]$Name, [string]$ServiceDir, [string]$Exe) {
  New-Item -ItemType Directory -Force -Path (Split-Path $Exe -Parent) | Out-Null
  Write-Host "[$Name] building"
  Push-Location $ServiceDir
  try {
    & go build -o $Exe ".\cmd\$Name"
    if ($LASTEXITCODE -ne 0) {
      Write-Host "[$Name] build failed"
      return $false
    }
    return $true
  } finally {
    Pop-Location
  }
}

function Start-BuiltService([string]$Name, [string]$ServiceDir, [string]$Exe) {
  Write-Host "[$Name] starting $Exe"
  Set-ServiceEnvironment $Name
  return Start-Process -FilePath $Exe -WorkingDirectory $ServiceDir -NoNewWindow -PassThru
}

function New-ServiceState([string]$Name) {
  Assert-Service $Name
  $serviceDir = Join-Path $Root "apps\$Name"
  return [PSCustomObject]@{
    Name = $Name
    Dir = $serviceDir
    Exe = Join-Path $serviceDir ".dev\$Name.exe"
    Stamp = ""
    Process = $null
  }
}

function Watch-Service([string]$Name) {
  Assert-Service $Name
  Set-ServiceEnvironment $Name

  $serviceDir = Join-Path $Root "apps\$Name"
  $exe = Join-Path $serviceDir ".dev\$Name.exe"
  $stamp = ""
  $process = $null

  try {
    while ($true) {
      $nextStamp = Get-ServiceStamp $serviceDir
      if ($nextStamp -ne $stamp) {
        Start-Sleep -Milliseconds 250
        $nextStamp = Get-ServiceStamp $serviceDir
        $stamp = $nextStamp
        Stop-RunningProcess $Name $process
        if (Build-Service $Name $serviceDir $exe) {
          $process = Start-BuiltService $Name $serviceDir $exe
        }
      }

      if ($null -ne $process -and $process.HasExited) {
        Write-Host "[$Name] exited with code $($process.ExitCode); waiting for changes"
        $process = $null
      }

      Start-Sleep -Seconds 1
    }
  } finally {
    Stop-RunningProcess $Name $process
  }
}

function Watch-Services([string[]]$Names) {
  $states = $Names | ForEach-Object { New-ServiceState $_ }
  Write-Host "dev services: $($Names -join ', ')"
  Write-Host "Press Ctrl+C to stop all services."

  try {
    while ($true) {
      foreach ($state in $states) {
        $nextStamp = Get-ServiceStamp $state.Dir
        if ($nextStamp -ne $state.Stamp) {
          Start-Sleep -Milliseconds 250
          $state.Stamp = Get-ServiceStamp $state.Dir
          Stop-RunningProcess $state.Name $state.Process
          if (Build-Service $state.Name $state.Dir $state.Exe) {
            $state.Process = Start-BuiltService $state.Name $state.Dir $state.Exe
          }
        }

        if ($null -ne $state.Process -and $state.Process.HasExited) {
          Write-Host "[$($state.Name)] exited with code $($state.Process.ExitCode); waiting for changes"
          $state.Process = $null
        }
      }

      Start-Sleep -Seconds 1
    }
  } finally {
    foreach ($state in $states) {
      Stop-RunningProcess $state.Name $state.Process
    }
  }
}

if ($Service) {
  Watch-Service $Service
  exit 0
}

if (-not $Services -or $Services.Count -eq 0) {
  $Services = $DefaultServices
}

if ($Windows) {
  Start-ServiceWindows $Services
  exit 0
}

if ($Tabs) {
  Start-ServiceTabs $Services
  exit 0
}

Watch-Services $Services
