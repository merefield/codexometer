[CmdletBinding()]
param(
    [string]$Version,
    [string]$BinDir,
    [string]$Repository,
    [string]$GitHubUrl,
    [string]$GitHubApiUrl
)

$ErrorActionPreference = "Stop"
$ProgressPreference = "SilentlyContinue"
Set-StrictMode -Version 2.0

function Get-Setting {
    param(
        [string]$Value,
        [string]$EnvironmentName,
        [string]$DefaultValue
    )

    if (-not [string]::IsNullOrWhiteSpace($Value)) {
        return $Value
    }
    $environmentValue = [Environment]::GetEnvironmentVariable($EnvironmentName)
    if (-not [string]::IsNullOrWhiteSpace($environmentValue)) {
        return $environmentValue
    }
    return $DefaultValue
}

function Fail {
    param([string]$Message)
    throw "codexometer installer: $Message"
}

$Version = Get-Setting $Version "CODEXOMETER_VERSION" "latest"
$Repository = Get-Setting $Repository "CODEXOMETER_REPOSITORY" "merefield/codexometer"
$GitHubUrl = (Get-Setting $GitHubUrl "CODEXOMETER_GITHUB_URL" "https://github.com").TrimEnd("/")
$GitHubApiUrl = (Get-Setting $GitHubApiUrl "CODEXOMETER_GITHUB_API_URL" "https://api.github.com").TrimEnd("/")

if ([string]::IsNullOrWhiteSpace($BinDir)) {
    $BinDir = [Environment]::GetEnvironmentVariable("CODEXOMETER_BIN_DIR")
}
if ([string]::IsNullOrWhiteSpace($BinDir)) {
    if (-not [string]::IsNullOrWhiteSpace($env:LOCALAPPDATA)) {
        $BinDir = Join-Path $env:LOCALAPPDATA "Programs\codexometer\bin"
    } else {
        $BinDir = Join-Path $HOME ".local\bin"
    }
}

if ($Repository -notmatch '^[A-Za-z0-9._-]+/[A-Za-z0-9._-]+$') {
    Fail "CODEXOMETER_REPOSITORY must have the form owner/repository"
}
if ([string]::IsNullOrWhiteSpace($BinDir)) {
    Fail "CODEXOMETER_BIN_DIR must not be empty"
}
if ((Test-Path -LiteralPath $BinDir) -and -not (Test-Path -LiteralPath $BinDir -PathType Container)) {
    Fail "CODEXOMETER_BIN_DIR exists and is not a directory: $BinDir"
}
if ($Version -ne "latest" -and $Version -notmatch '^[A-Za-z0-9._-]+$') {
    Fail "invalid release tag: $Version"
}

$architecture = [Runtime.InteropServices.RuntimeInformation]::OSArchitecture.ToString()
switch ($architecture) {
    "X64" { $releaseArch = "amd64" }
    "Arm64" { $releaseArch = "arm64" }
    default { Fail "unsupported architecture: $architecture" }
}

[Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12
$headers = @{ "User-Agent" = "codexometer-release-installer" }
$temporaryDirectory = Join-Path ([IO.Path]::GetTempPath()) ("codexometer-release-install-" + [Guid]::NewGuid().ToString("N"))
New-Item -ItemType Directory -Path $temporaryDirectory | Out-Null

try {
    $releaseTag = $Version
    if ($releaseTag -eq "latest") {
        Write-Host "Resolving the latest Codexometer release..."
        $latestUrl = "$GitHubApiUrl/repos/$Repository/releases/latest"
        try {
            $release = Invoke-RestMethod -Uri $latestUrl -Headers $headers
        } catch {
            Fail "could not resolve the latest release: $($_.Exception.Message)"
        }
        if ($release -is [string]) {
            try {
                $release = $release | ConvertFrom-Json
            } catch {
                Fail "could not parse the latest release response: $($_.Exception.Message)"
            }
        }
        $releaseTag = [string]$release.tag_name
        if ([string]::IsNullOrWhiteSpace($releaseTag)) {
            Fail "could not determine the latest release tag"
        }
    }

    if ($releaseTag -notmatch '^[A-Za-z0-9._-]+$') {
        Fail "invalid release tag: $releaseTag"
    }
    $releaseVersion = $releaseTag -replace '^v', ''
    if ([string]::IsNullOrWhiteSpace($releaseVersion)) {
        Fail "invalid release tag: $releaseTag"
    }

    $archiveName = "codexometer_${releaseVersion}_windows_${releaseArch}.zip"
    $releaseUrl = "$GitHubUrl/$Repository/releases/download/$releaseTag"
    $archivePath = Join-Path $temporaryDirectory $archiveName
    $checksumsPath = Join-Path $temporaryDirectory "checksums.txt"

    Write-Host "Downloading Codexometer $releaseTag for windows/$releaseArch..."
    try {
        Invoke-WebRequest -Uri "$releaseUrl/$archiveName" -Headers $headers -OutFile $archivePath -UseBasicParsing
        Invoke-WebRequest -Uri "$releaseUrl/checksums.txt" -Headers $headers -OutFile $checksumsPath -UseBasicParsing
    } catch {
        Fail "release download failed: $($_.Exception.Message)"
    }

    $matchingChecksums = @(
        Get-Content -LiteralPath $checksumsPath | ForEach-Object {
            if ($_ -match '^([0-9A-Fa-f]{64})\s+\*?(.+)$' -and $Matches[2] -eq $archiveName) {
                $Matches[1]
            }
        }
    )
    if ($matchingChecksums.Count -ne 1) {
        Fail "checksums.txt does not contain exactly one valid checksum for $archiveName"
    }

    $expectedHash = $matchingChecksums[0].ToLowerInvariant()
    $actualHash = (Get-FileHash -LiteralPath $archivePath -Algorithm SHA256).Hash.ToLowerInvariant()
    if ($actualHash -ne $expectedHash) {
        Fail "SHA-256 checksum verification failed for $archiveName"
    }
    Write-Host "Verified the release checksum."

    Add-Type -AssemblyName System.IO.Compression.FileSystem
    $archive = [IO.Compression.ZipFile]::OpenRead($archivePath)
    try {
        $binaryEntries = @($archive.Entries | Where-Object { $_.FullName -eq "codexometer.exe" })
        if ($binaryEntries.Count -ne 1) {
            Fail "release archive does not contain exactly one root-level codexometer.exe binary"
        }
        $candidate = Join-Path $temporaryDirectory "codexometer.exe"
        $source = $binaryEntries[0].Open()
        try {
            $destination = [IO.File]::Create($candidate)
            try {
                $source.CopyTo($destination)
            } finally {
                $destination.Dispose()
            }
        } finally {
            $source.Dispose()
        }
    } finally {
        $archive.Dispose()
    }

    $versionOutput = (& $candidate --version 2>&1 | Out-String).Trim()
    if ($LASTEXITCODE -ne 0) {
        Fail "the downloaded codexometer binary failed its version check"
    }
    if ($versionOutput -ne "codexometer $releaseVersion") {
        Fail "the downloaded binary reported an unexpected version: $versionOutput"
    }

    try {
        New-Item -ItemType Directory -Path $BinDir -Force | Out-Null
        $target = Join-Path $BinDir "codexometer.exe"
        $stagedTarget = Join-Path $BinDir (".codexometer-" + [Guid]::NewGuid().ToString("N") + ".exe")
        Copy-Item -LiteralPath $candidate -Destination $stagedTarget
        Move-Item -LiteralPath $stagedTarget -Destination $target -Force
    } catch {
        Fail "could not install into ${BinDir}: $($_.Exception.Message); set CODEXOMETER_BIN_DIR to a writable directory"
    }

    Write-Host "Installed codexometer to $target ($versionOutput)."
    $pathEntries = @($env:PATH -split ';')
    if ($pathEntries -notcontains $BinDir) {
        Write-Host "Add $BinDir to PATH before invoking codexometer."
    }
} finally {
    if (Test-Path -LiteralPath $temporaryDirectory) {
        Remove-Item -LiteralPath $temporaryDirectory -Recurse -Force
    }
}
