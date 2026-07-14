# Remove-PostgresRestoreTest.ps1 — drops exactly ONE explicitly named
# throwaway test database. Only the two unambiguous test prefixes used by
# the backup drill are accepted; every other name (especially pjsk,
# postgres, template0/1) is refused. No pattern matching, no bulk deletes.
#
# Passwords are NEVER accepted as parameters, printed, or logged.
[CmdletBinding()]
param(
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

$forbiddenNames = @('pjsk', 'postgres', 'template0', 'template1')
if ($forbiddenNames -contains $TargetDatabase.ToLowerInvariant()) {
    Fail "Refusing to drop protected database '$TargetDatabase'."
}
if ($TargetDatabase -notmatch '^pjsk_(restore|backup_source)_test_[a-z0-9_]+$' -or $TargetDatabase.Length -gt 63) {
    Fail "TargetDatabase must be a pjsk_restore_test_* or pjsk_backup_source_test_* database."
}

$psql = Join-Path $PostgresBin 'psql.exe'
$dropDb = Join-Path $PostgresBin 'dropdb.exe'
foreach ($tool in @($psql, $dropDb)) {
    if (-not (Test-Path -LiteralPath $tool)) {
        Fail "PostgreSQL tool not found: $tool"
    }
}

$connectionArguments = @('--host', $HostName, '--port', "$Port", '-w')
if ($Username) { $connectionArguments += @('--username', $Username) }

if ($DryRun) {
    Write-Output "DRY RUN — no external command will be executed."
    Write-Output "  would terminate connections to and drop: $TargetDatabase"
    exit 0
}

$exists = & $psql @connectionArguments '--dbname', 'postgres', '-X', '-A', '-t', '-c', "select 1 from pg_database where datname = '$TargetDatabase'"
if ($LASTEXITCODE -ne 0) {
    Fail "Could not check whether '$TargetDatabase' exists."
}
if ("$exists".Trim() -ne '1') {
    Write-Output "Database '$TargetDatabase' does not exist; nothing to remove."
    exit 0
}

# Terminate lingering connections, then drop.
& $psql @connectionArguments '--dbname', 'postgres', '-X', '-c', "select pg_terminate_backend(pid) from pg_stat_activity where datname = '$TargetDatabase' and pid <> pg_backend_pid()" | Out-Null
if ($LASTEXITCODE -ne 0) {
    Fail "Could not terminate connections to '$TargetDatabase'."
}

& $dropDb @connectionArguments -- $TargetDatabase
if ($LASTEXITCODE -ne 0) {
    Fail "dropdb failed for '$TargetDatabase'."
}

$still = & $psql @connectionArguments '--dbname', 'postgres', '-X', '-A', '-t', '-c', "select 1 from pg_database where datname = '$TargetDatabase'"
if ($LASTEXITCODE -ne 0 -or "$still".Trim() -eq '1') {
    Fail "Database '$TargetDatabase' still exists after dropdb."
}

Write-Output "Dropped test database '$TargetDatabase'."
exit 0
