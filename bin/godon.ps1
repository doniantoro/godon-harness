# godon launcher for Windows PowerShell (5.1 and later) — used where no Git
# Bash is available, such as Codex hooks on Windows (commandWindows).
#
# Same contract as bin/godon: fetch the release pinned in ..\VERSION for this
# CPU, verify it against checksums.txt, cache it, run it with the arguments
# and forward stdin (hook payloads arrive on stdin).
#
# Environment: GODON_BIN, GODON_CACHE, GODON_RELEASE_URL — see bin/godon.
$ErrorActionPreference = 'Stop'
$repo = 'doniantoro/godon-harness'
$isHook = ($args.Count -gt 0 -and $args[0] -eq 'hook')

function Invoke-Godon([string]$bin, [object[]]$arguments) {
    if ([Console]::IsInputRedirected) {
        $payload = [Console]::In.ReadToEnd()
        $payload | & $bin @arguments
    } else {
        & $bin @arguments
    }
    exit $LASTEXITCODE
}

function Fail([string]$message) {
    [Console]::Error.WriteLine("godon: $message")
    [Console]::Error.WriteLine("godon: fetch it once with network access by running: powershell -File `"$PSCommandPath`" version")
    [Console]::Error.WriteLine("godon: or point GODON_BIN at a godon binary (go install github.com/$repo/cmd/godon@v$version)")
    # A hook must not break the agent's turn because godon could not be fetched.
    if ($isHook) { exit 0 }
    exit 4
}

if ($env:GODON_BIN) { Invoke-Godon $env:GODON_BIN $args }

$version = (Get-Content -Raw (Join-Path $PSScriptRoot '..\VERSION')).Trim()

switch ($env:PROCESSOR_ARCHITECTURE) {
    'AMD64' { $arch = 'amd64' }
    'ARM64' { $arch = 'arm64' }
    default { Fail "unsupported CPU: $env:PROCESSOR_ARCHITECTURE" }
}
$asset = "godon_windows_$arch.exe"

$cacheRoot = $env:GODON_CACHE
if (-not $cacheRoot) { $cacheRoot = Join-Path $env:LOCALAPPDATA 'godon' }
$cache = Join-Path $cacheRoot $version
$bin = Join-Path $cache $asset

if (-not (Test-Path $bin)) {
    $base = $env:GODON_RELEASE_URL
    if (-not $base) { $base = "https://github.com/$repo/releases/download/v$version" }
    New-Item -ItemType Directory -Force -Path $cache | Out-Null
    $tmp = "$bin.tmp.$PID"
    $sums = Join-Path $cache "checksums.txt.$PID"
    try {
        [Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12
        $ProgressPreference = 'SilentlyContinue'
        try { Invoke-WebRequest -UseBasicParsing -Uri "$base/$asset" -OutFile $tmp }
        catch { Fail "could not download $base/$asset" }
        try { Invoke-WebRequest -UseBasicParsing -Uri "$base/checksums.txt" -OutFile $sums }
        catch { Fail "could not download $base/checksums.txt" }

        $want = $null
        foreach ($line in Get-Content $sums) {
            $parts = $line -split '\s+'
            if ($parts.Count -ge 2 -and ($parts[1] -eq $asset -or $parts[1] -eq "*$asset")) { $want = $parts[0].ToLower() }
        }
        if (-not $want) { Fail "checksums.txt has no entry for $asset" }
        $got = (Get-FileHash -Algorithm SHA256 $tmp).Hash.ToLower()
        if ($want -ne $got) { Fail "checksum mismatch for $asset (want $want, got $got)" }

        Move-Item -Force $tmp $bin
    }
    finally {
        Remove-Item -Force -ErrorAction SilentlyContinue $tmp, $sums
    }
}

Invoke-Godon $bin $args
