# Backup-Postgres.ps1 — logical backup of one PostgreSQL database in custom
# format, with atomic .partial writing, pg_restore --list verification,
# SHA-256, and a non-sensitive metadata JSON.
#
# Passwords are NEVER accepted as parameters, printed, or logged. Provide
# credentials via an already-set process-level PGPASSWORD, a pgpass.conf, or
# the interactive prompt of the PostgreSQL tools themselves.
[CmdletBinding()]
param(
    [Parameter(Mandatory)]
    [string]$DatabaseName,
    [Parameter(Mandatory)]
    [string]$BackupRoot,
    [string]$PostgresBin = "D:\PostgreSQL\18\bin",
    [string]$HostName = "127.0.0.1",
    [int]$Port = 5432,
    [string]$Username,
    [string]$OutputPath,
    [switch]$RequireIsolatedSource,
    [switch]$DryRun
)

$ErrorActionPreference = 'Stop'

function Fail([string]$Message) {
    Write-Error $Message
    exit 1
}

# --- validate database name (identifier only; never interpolated into SQL) ---
if ($DatabaseName -notmatch '^[a-z][a-z0-9_]{0,62}$') {
    Fail "DatabaseName must match ^[a-z][a-z0-9_]{0,62}$ (lowercase identifier)."
}
if ($RequireIsolatedSource -and $DatabaseName -notmatch '^pjsk_backup_source_test_[a-z0-9_]+$') {
    Fail "RequireIsolatedSource only permits a pjsk_backup_source_test_* database."
}

# --- validate tools ---
$pgDump = Join-Path $PostgresBin 'pg_dump.exe'
$pgRestore = Join-Path $PostgresBin 'pg_restore.exe'
foreach ($tool in @($pgDump, $pgRestore)) {
    if (-not (Test-Path -LiteralPath $tool)) {
        Fail "PostgreSQL tool not found: $tool"
    }
}

# --- validate backup root: absolute, outside this Git repository ---
if (-not [System.IO.Path]::IsPathRooted($BackupRoot)) {
    Fail "BackupRoot must be an absolute path outside the repository."
}
$backupRootFull = [System.IO.Path]::GetFullPath($BackupRoot)
$repoRoot = [System.IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..\..'))
$repoPrefix = $repoRoot.TrimEnd('\') + '\'
if ($backupRootFull.TrimEnd('\') -eq $repoRoot.TrimEnd('\') -or
    ($backupRootFull.TrimEnd('\') + '\').StartsWith($repoPrefix, [System.StringComparison]::OrdinalIgnoreCase)) {
    Fail "BackupRoot must not be inside the Git repository ($repoRoot)."
}

# --- output layout ---
$stamp = Get-Date
if ($OutputPath) {
    if (-not [System.IO.Path]::IsPathRooted($OutputPath)) {
        Fail "OutputPath must be an absolute .dump or .backup path."
    }
    $dumpPath = [System.IO.Path]::GetFullPath($OutputPath)
    $targetDirectory = [System.IO.Path]::GetDirectoryName($dumpPath)
    $extension = [System.IO.Path]::GetExtension($dumpPath)
    if ($extension -notin @('.dump', '.backup')) {
        Fail "OutputPath must use the .dump or .backup extension."
    }
    $backupPrefix = $backupRootFull.TrimEnd('\') + '\'
    if (-not ($targetDirectory.TrimEnd('\') + '\').StartsWith($backupPrefix, [System.StringComparison]::OrdinalIgnoreCase)) {
        Fail "OutputPath must be located under BackupRoot."
    }
} else {
    $targetDirectory = Join-Path $backupRootFull (Join-Path $stamp.ToString('yyyy') $stamp.ToString('MM'))
    $baseName = 'pjsk-' + $stamp.ToString('yyyyMMdd-HHmmss')
    $dumpPath = Join-Path $targetDirectory ($baseName + '.dump')
}
$partialPath = $dumpPath + '.partial'
$metadataPath = Join-Path $targetDirectory (([System.IO.Path]::GetFileNameWithoutExtension($dumpPath)) + '.metadata.json')
$metadataPartialPath = $metadataPath + '.partial'

foreach ($candidate in @($dumpPath, $partialPath, $metadataPath, $metadataPartialPath)) {
    if (Test-Path -LiteralPath $candidate) {
        Fail "Refusing to overwrite an existing backup, partial, or metadata file: $candidate"
    }
}

# Build one argument array for both the preview and the real command.
$dumpArguments = @(
    '--format=custom',
    '--no-owner',
    '--no-privileges',
    '--verbose',
    '-w',
    '--host', $HostName,
    '--port', "$Port",
    '--file', $partialPath
)
if ($Username) { $dumpArguments += @('--username', $Username) }
$dumpArguments += $DatabaseName

if ($DryRun) {
    Write-Output "DRY RUN — no external command will be executed."
    Write-Output "  database        : $DatabaseName"
    Write-Output "  isolated source : $RequireIsolatedSource"
    Write-Output "  host:port       : ${HostName}:$Port"
    Write-Output "  pg_dump         : $pgDump"
    Write-Output "  expected type   : pg_dump custom-format logical backup"
    Write-Output "  expected args   : --format=custom --no-owner --no-privileges --verbose -w --host $HostName --port $Port --file `"$partialPath`" --username $Username $DatabaseName"
    Write-Output "  final output    : $dumpPath"
    Write-Output "  atomic partial  : $partialPath"
    Write-Output "  metadata        : $metadataPath"
    Write-Output "  publish         : verified partials are atomically renamed to final paths"
    Write-Output "  overwrite check : final, partial, metadata, and metadata partial do not exist"
    Write-Output "  verify          : pg_restore --list + SHA-256 + metadata JSON (real run only)"
    exit 0
}

New-Item -ItemType Directory -Force -Path $targetDirectory | Out-Null

function Remove-Partial {
    if (Test-Path -LiteralPath $partialPath) {
        Remove-Item -LiteralPath $partialPath -Force -Confirm:$false
    }
    if (Test-Path -LiteralPath $metadataPartialPath) {
        Remove-Item -LiteralPath $metadataPartialPath -Force -Confirm:$false
    }
}

# --- run pg_dump to the .partial file ---
& $pgDump @dumpArguments
if ($LASTEXITCODE -ne 0) {
    Remove-Partial
    Fail "pg_dump failed with exit code $LASTEXITCODE; partial file removed."
}

# --- verify the dump is readable ---
$listOutput = & $pgRestore '--list' $partialPath
if ($LASTEXITCODE -ne 0 -or -not $listOutput) {
    Remove-Partial
    Fail "pg_restore --list could not read the dump; partial file removed."
}
$objectLineCount = @($listOutput | Where-Object { $_ -match '^\d+;' }).Count

# --- hash and prepare atomic metadata before promoting either final file ---
$hash = (Get-FileHash -LiteralPath $partialPath -Algorithm SHA256).Hash
$sizeBytes = (Get-Item -LiteralPath $partialPath).Length
if ($sizeBytes -le 0) {
    Remove-Partial
    Fail "Backup file is empty."
}

$clientVersion = (& $pgDump '--version') -join ' '
$scriptHash = (Get-FileHash -LiteralPath $PSCommandPath -Algorithm SHA256).Hash
$fixtureExpectedRowCounts = [ordered]@{
    admins            = 1
    users             = 2
    projects          = 1
    products          = 2
    orders            = 2
    order_items       = 2
    payments          = 1
    payment_items     = 1
    schema_migrations = 18
}
$metadata = [ordered]@{
    schemaVersion            = 1
    createdAtUtc             = $stamp.ToUniversalTime().ToString('o')
    clientVersion            = $clientVersion
    sourceDatabaseName       = $DatabaseName
    isolatedTestBackup       = [bool]$RequireIsolatedSource
    dumpFormat               = 'custom'
    dumpFileName             = [System.IO.Path]::GetFileName($dumpPath)
    dumpSizeBytes            = $sizeBytes
    dumpSha256               = $hash
    backupScriptSha256       = $scriptHash
    fixtureExpectedRowCounts = $fixtureExpectedRowCounts
    objectCount              = $objectLineCount
}
$dumpPublished = $false
try {
    $metadata | ConvertTo-Json | Out-File -LiteralPath $metadataPartialPath -Encoding utf8
    $metadataCheck = Get-Content -LiteralPath $metadataPartialPath -Raw | ConvertFrom-Json
    if ($metadataCheck.sourceDatabaseName -ne $DatabaseName -or
        $metadataCheck.dumpSha256 -ne $hash -or
        $metadataCheck.dumpSizeBytes -ne $sizeBytes -or
        $metadataCheck.dumpFormat -ne 'custom' -or
        -not $metadataCheck.isolatedTestBackup) {
        throw "metadata partial validation failed"
    }
    Move-Item -LiteralPath $partialPath -Destination $dumpPath
    $dumpPublished = $true
    Move-Item -LiteralPath $metadataPartialPath -Destination $metadataPath
} catch {
    Remove-Partial
    if ($dumpPublished -and (Test-Path -LiteralPath $dumpPath)) {
        Remove-Item -LiteralPath $dumpPath -Force -Confirm:$false
    }
    Fail "Could not atomically publish the backup and metadata."
}

Write-Output "Backup complete."
Write-Output "  dump     : $dumpPath"
Write-Output "  size     : $sizeBytes bytes"
Write-Output "  sha256   : $hash"
Write-Output "  objects  : $objectLineCount"
Write-Output "  metadata : $metadataPath"
exit 0
