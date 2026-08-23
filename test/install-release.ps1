$ErrorActionPreference = "Stop"
Set-StrictMode -Version 2.0

$repositoryRoot = Split-Path -Parent $PSScriptRoot
$installer = Join-Path $repositoryRoot "install-release.ps1"
$testRoot = Join-Path ([IO.Path]::GetTempPath()) ("codexometer-release-installer-test-" + [Guid]::NewGuid().ToString("N"))
$serverRoot = Join-Path $testRoot "server"
$packageDirectory = Join-Path $testRoot "package"
$installDirectory = Join-Path $testRoot "installed"
$badInstallDirectory = Join-Path $testRoot "bad-installed"
$releaseTag = "v1.2.3"
$releaseVersion = "1.2.3"
$assetName = "codexometer_1.2.3_windows_amd64.zip"
$releaseDirectory = Join-Path $serverRoot "merefield\codexometer\releases\download\$releaseTag"
$latestDirectory = Join-Path $serverRoot "repos\merefield\codexometer\releases"
$fixtureBinary = Join-Path $packageDirectory "codexometer.exe"
$archivePath = Join-Path $releaseDirectory $assetName
$checksumsPath = Join-Path $releaseDirectory "checksums.txt"
$serverProcess = $null
$savedEnvironment = @{}

function Write-Utf8File {
    param([string]$Path, [string]$Content)
    [IO.File]::WriteAllText($Path, $Content, (New-Object Text.UTF8Encoding($false)))
}

function Invoke-Installer {
    param([string]$Destination)
    $hostExecutable = (Get-Process -Id $PID).Path
    $output = & $hostExecutable -NoProfile -ExecutionPolicy Bypass -File $installer -BinDir $Destination 2>&1
    return @{
        Status = $LASTEXITCODE
        Output = ($output | Out-String).Trim()
    }
}

try {
    New-Item -ItemType Directory -Path $packageDirectory, $releaseDirectory, $latestDirectory | Out-Null

    Push-Location $repositoryRoot
    try {
        & go build -trimpath -ldflags "-X github.com/merefield/codexometer/internal/version.buildVersion=$releaseTag" -o $fixtureBinary .
        if ($LASTEXITCODE -ne 0) {
            throw "failed to build the Windows installer fixture"
        }
    } finally {
        Pop-Location
    }

    Compress-Archive -LiteralPath $fixtureBinary -DestinationPath $archivePath
    $archiveHash = (Get-FileHash -LiteralPath $archivePath -Algorithm SHA256).Hash.ToLowerInvariant()
    Write-Utf8File $checksumsPath "$archiveHash  $assetName`n"
    Write-Utf8File (Join-Path $latestDirectory "latest") '{"tag_name":"v1.2.3"}'

    $listener = [Net.Sockets.TcpListener]::new([Net.IPAddress]::Loopback, 0)
    $listener.Start()
    $port = ([Net.IPEndPoint]$listener.LocalEndpoint).Port
    $listener.Stop()

    $pythonCommand = Get-Command python -ErrorAction SilentlyContinue
    if ($null -eq $pythonCommand) {
        $pythonCommand = Get-Command python3 -ErrorAction Stop
    }
    $serverProcess = Start-Process -FilePath $pythonCommand.Source -ArgumentList @("-m", "http.server", "$port", "--bind", "127.0.0.1") -WorkingDirectory $serverRoot -PassThru
    $baseUrl = "http://127.0.0.1:$port"
    $ready = $false
    for ($attempt = 0; $attempt -lt 50; $attempt++) {
        try {
            Invoke-WebRequest -Uri "$baseUrl/repos/merefield/codexometer/releases/latest" -UseBasicParsing | Out-Null
            $ready = $true
            break
        } catch {
            Start-Sleep -Milliseconds 100
        }
    }
    if (-not $ready) {
        throw "fixture HTTP server did not start"
    }

    foreach ($name in @("CODEXOMETER_VERSION", "CODEXOMETER_REPOSITORY", "CODEXOMETER_GITHUB_URL", "CODEXOMETER_GITHUB_API_URL")) {
        $savedEnvironment[$name] = [Environment]::GetEnvironmentVariable($name)
    }
    $env:CODEXOMETER_VERSION = "latest"
    $env:CODEXOMETER_REPOSITORY = "merefield/codexometer"
    $env:CODEXOMETER_GITHUB_URL = $baseUrl
    $env:CODEXOMETER_GITHUB_API_URL = $baseUrl

    $result = Invoke-Installer $installDirectory
    if ($result.Status -ne 0) {
        throw "release installer failed:`n$($result.Output)"
    }
    if ($result.Output -notmatch 'Verified the release checksum\.') {
        throw "release installer did not report checksum verification"
    }
    $installedBinary = Join-Path $installDirectory "codexometer.exe"
    if (-not (Test-Path -LiteralPath $installedBinary -PathType Leaf)) {
        throw "release installer did not install codexometer.exe"
    }
    $versionOutput = (& $installedBinary --version 2>&1 | Out-String).Trim()
    if ($versionOutput -ne "codexometer $releaseVersion") {
        throw "installed binary reported an unexpected version: $versionOutput"
    }

    Write-Utf8File $checksumsPath "$('0' * 64)  $assetName`n"
    $badResult = Invoke-Installer $badInstallDirectory
    if ($badResult.Status -eq 0) {
        throw "release installer accepted a checksum mismatch"
    }
    if ($badResult.Output -notmatch 'SHA-256 checksum verification failed') {
        throw "release installer returned the wrong checksum failure: $($badResult.Output)"
    }
    if (Test-Path -LiteralPath (Join-Path $badInstallDirectory "codexometer.exe")) {
        throw "release installer installed a checksum-mismatched binary"
    }

    Write-Host "Windows release installer tests passed."
} finally {
    foreach ($name in $savedEnvironment.Keys) {
        [Environment]::SetEnvironmentVariable($name, $savedEnvironment[$name])
    }
    if ($null -ne $serverProcess -and -not $serverProcess.HasExited) {
        Stop-Process -Id $serverProcess.Id -Force
    }
    if (Test-Path -LiteralPath $testRoot) {
        Remove-Item -LiteralPath $testRoot -Recurse -Force
    }
}

# The expected checksum-failure child leaves LASTEXITCODE set to 1. Reaching
# this point means every assertion passed, so report success explicitly.
exit 0
