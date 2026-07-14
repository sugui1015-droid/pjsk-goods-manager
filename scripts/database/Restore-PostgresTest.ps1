# Restore-PostgresTest.ps1 — restores a custom-format dump into a BRAND-NEW
# test database. Restoring into an existing or production database is
# refused by design: the target name must start with pjsk_restore_test_ and
# must not already exist. --clean and --create are never used.
#
# Passwords are NEVER accepted as parameters, printed, or logged.
[CmdletBinding()]
param(
    [Parameter(Mandatory)]
    [string]$BackupFile,
    [Parameter(Mandatory)]
    [string]$TargetDatabase,
    [string]$PostgresBin = "D:\PostgreSQL\18\bin",
    [string]$HostName = "127.0.0.1",
    [int]$Port = 5432,
    [string]$Username,
    [switch]$DryRun
)

$ErrorActionPreference = 'Stop'

function Fail([string]$Message) {
    Write-Error $Message
    exit 1
}

# --- target database name protection ---
$forbiddenNames = @('pjsk', 'postgres', 'template0', 'template1')
if ($forbiddenNames -contains $TargetDatabase.ToLowerInvariant()) {
    Fail "Refusing to touch protected database name '$TargetDatabase'."
}
if ($TargetDatabase -notmatch '^pjsk_restore_test_[a-z0-9_]+$' -or $TargetDatabase.Length -gt 63) {
    Fail "TargetDatabase must match ^pjsk_restore_test_[a-z0-9_]+$ and be at most 63 characters."
}

# --- backup file checks ---
if (-not (Test-Path -LiteralPath $BackupFile)) {
    Fail "Backup file not found: $BackupFile"
}
if ([System.IO.Path]::GetExtension($BackupFile) -ne '.dump') {
    Fail "Backup file must have the .dump extension."
}

# --- tools ---
$pgRestore = Join-Path $PostgresBin 'pg_restore.exe'
$createDb = Join-Path $PostgresBin 'createdb.exe'
$psql = Join-Path $PostgresBin 'psql.exe'
foreach ($tool in @($pgRestore, $createDb, $psql)) {
    if (-not (Test-Path -LiteralPath $tool)) {
        Fail "PostgreSQL tool not found: $tool"
    }
}

$connectionArguments = @('--host', $HostName, '--port', "$Port", '-w')
if ($Username) { $connectionArguments += @('--username', $Username) }

if ($DryRun) {
    Write-Output "DRY RUN — no external command will be executed."
    Write-Output "  backup file : $BackupFile"
    Write-Output "  target      : $TargetDatabase (must not exist; will be created empty)"
    Write-Output "  host:port   : ${HostName}:$Port"
    Write-Output "  restore     : pg_restore --no-owner --no-privileges --exit-on-error --single-transaction"
    Write-Output "  never used  : --clean / --create / restore into the source database"
    exit 0
}

# --- verify the dump is readable before touching anything ---
& $pgRestore '--list' $BackupFile | Out-Null
if ($LASTEXITCODE -ne 0) {
    Fail "pg_restore --list could not read $BackupFile"
}

# --- the target database must not exist ---
$exists = & $psql @connectionArguments '--dbname', 'postgres', '-X', '-A', '-t', '-c', "select 1 from pg_database where datname = '$TargetDatabase'"
if ($LASTEXITCODE -ne 0) {
    Fail "Could not check whether the target database exists."
}
if ("$exists".Trim() -eq '1') {
    Fail "Target database '$TargetDatabase' already exists; refusing to touch it. Use a fresh name."
}

# --- create the empty target database ---
& $createDb @connectionArguments '--template' 'template0' -- $TargetDatabase
if ($LASTEXITCODE -ne 0) {
    Fail "createdb failed for '$TargetDatabase'."
}

# --- restore ---
$restoreArguments = $connectionArguments + @(
    '--dbname', $TargetDatabase,
    '--no-owner',
    '--no-privileges',
    '--exit-on-error',
    '--single-transaction',
    '--verbose',
    $BackupFile
)
& $pgRestore @restoreArguments
if ($LASTEXITCODE -ne 0) {
    Write-Warning "pg_restore failed. The target database '$TargetDatabase' is left in a FAILED state for diagnosis; it was created by this run and contains no production data. Remove it with Remove-PostgresRestoreTest.ps1 and retry with a NEW target name."
    exit 1
}

Write-Output "Restore complete into new test database '$TargetDatabase'."
exit 0
