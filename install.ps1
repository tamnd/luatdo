# Install luatdo on Windows.
#
#   irm https://raw.githubusercontent.com/tamnd/luatdo/main/install.ps1 | iex
#
# To get the graph as well, download it first and pass the switch, because a
# script piped into iex has no way to receive arguments:
#
#   & ([scriptblock]::Create((irm https://raw.githubusercontent.com/tamnd/luatdo/main/install.ps1))) -WithData
#
# Written for Windows PowerShell 5.1, which is what a Windows machine has before
# anybody installs anything. That rules out the ternary operator, the null
# coalescing operator and everything else added in PowerShell 7, all of which
# fail at parse time and so would break the script for people who never used the
# feature.

[CmdletBinding()]
param(
    [string]$Version = $env:LUATDO_VERSION,
    [string]$BinDir = $env:LUATDO_BIN_DIR,
    [switch]$WithData
)

$ErrorActionPreference = 'Stop'
# Windows PowerShell 5.1 defaults to TLS 1.0 on older installs, and GitHub has
# not accepted that for years. The symptom without this line is an underlying
# connection error that says nothing about protocols.
[Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12
# Invoke-WebRequest draws a progress bar per read, which on a large download
# costs more time than the download.
$ProgressPreference = 'SilentlyContinue'

$repo = 'tamnd/luatdo'

function Fail($message) {
    Write-Host "install.ps1: $message" -ForegroundColor Red
    exit 1
}

$arch = 'amd64'
if ($env:PROCESSOR_ARCHITECTURE -eq 'ARM64') { $arch = 'arm64' }

if (-not $Version) {
    # The release page redirects to the newest tag, which answers this without
    # spending one of the sixty unauthenticated API calls an address gets per
    # hour.
    try {
        $head = Invoke-WebRequest -Uri "https://github.com/$repo/releases/latest" -MaximumRedirection 0 -ErrorAction SilentlyContinue
        $location = $head.Headers.Location
    } catch {
        $location = $_.Exception.Response.Headers['Location']
    }
    if ($location) { $Version = ($location -split '/tag/')[-1] }
}
if (-not $Version) {
    Fail 'could not work out the latest version, pass -Version vX.Y.Z'
}

# The tag carries a leading v and the archive name does not.
$number = $Version.TrimStart('v')
$archive = "luatdo_${number}_windows_${arch}.zip"
$base = "https://github.com/$repo/releases/download/$Version"

if (-not $BinDir) { $BinDir = Join-Path $env:LOCALAPPDATA 'luatdo\bin' }

$tmp = Join-Path ([IO.Path]::GetTempPath()) ("luatdo-" + [Guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $tmp -Force | Out-Null
try {
    Write-Host "installing luatdo $Version for windows/$arch"
    try {
        Invoke-WebRequest -Uri "$base/$archive" -OutFile (Join-Path $tmp $archive)
        Invoke-WebRequest -Uri "$base/checksums.txt" -OutFile (Join-Path $tmp 'checksums.txt')
    } catch {
        Fail "could not download $archive from release $Version"
    }

    # A binary downloaded over the network and then run is worth thirty two bytes
    # of checking.
    $sum = (Get-FileHash -Algorithm SHA256 -Path (Join-Path $tmp $archive)).Hash.ToLower()
    $want = $null
    foreach ($line in Get-Content (Join-Path $tmp 'checksums.txt')) {
        $parts = $line -split '\s+'
        if ($parts.Length -ge 2 -and $parts[-1] -eq $archive) { $want = $parts[0].ToLower() }
    }
    if (-not $want) { Fail "$archive is not listed in checksums.txt" }
    if ($sum -ne $want) { Fail "$archive has checksum $sum and the release says $want" }

    Expand-Archive -Path (Join-Path $tmp $archive) -DestinationPath $tmp -Force
    $exe = Join-Path $tmp 'luatdo.exe'
    if (-not (Test-Path $exe)) { Fail "$archive does not hold luatdo.exe" }

    New-Item -ItemType Directory -Path $BinDir -Force | Out-Null
    # Windows will not overwrite a running image, and the error it gives says
    # access denied without saying by what. Moving the old one aside first turns
    # an upgrade during a long run into something that works.
    $target = Join-Path $BinDir 'luatdo.exe'
    if (Test-Path $target) {
        $old = Join-Path $BinDir 'luatdo.exe.old'
        Remove-Item $old -Force -ErrorAction SilentlyContinue
        try { Move-Item $target $old -Force } catch { }
    }
    Copy-Item $exe $target -Force
    Write-Host "installed $target"
} finally {
    Remove-Item $tmp -Recurse -Force -ErrorAction SilentlyContinue
}

& (Join-Path $BinDir 'luatdo.exe') version

# The user PATH rather than the machine PATH, so this needs no administrator.
$userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
if (-not $userPath) { $userPath = '' }
$onPath = $false
foreach ($entry in $userPath -split ';') {
    if ($entry.TrimEnd('\') -eq $BinDir.TrimEnd('\')) { $onPath = $true }
}
if (-not $onPath) {
    [Environment]::SetEnvironmentVariable('Path', ($userPath.TrimEnd(';') + ";$BinDir").TrimStart(';'), 'User')
    # The registry is updated and this shell is not, so the person would type
    # luatdo and be told it is not recognised.
    $env:Path = "$env:Path;$BinDir"
    Write-Host "added $BinDir to your PATH, which new terminals will pick up"
}

if (-not $WithData) {
    Write-Host ''
    Write-Host 'to get the graph and a local Neo4j over it, run:'
    Write-Host '  luatdo neo4j install'
    exit 0
}

$runtime = $null
foreach ($name in @('podman', 'docker')) {
    if (Get-Command $name -ErrorAction SilentlyContinue) { $runtime = $name; break }
}
if (-not $runtime) {
    Fail '-WithData needs podman or docker and neither is installed, the binary is in place so install one and run luatdo neo4j install'
}

Write-Host ''
Write-Host 'fetching the published graph and loading it, which takes a while'
& (Join-Path $BinDir 'luatdo.exe') neo4j install
exit $LASTEXITCODE
