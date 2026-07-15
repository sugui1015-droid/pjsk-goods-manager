# Test-PostgresBackup.ps1 — structural verification of a restored TEST
# database, optionally compared against the isolated TEST source database.
# Prints only object names, counts, and pass/fail — never business rows,
# emails, CNs, query codes, or payment contents.
[CmdletBinding()]
param(
    [Parameter(Mandatory)]
    [string]$RestoredDatabase,
    [string]$SourceDatabase,
    [string]$PostgresBin = "D:\PostgreSQL\18\bin",
    [string]$HostName = "127.0.0.1",
    [int]$Port = 5432,
    [string]$Username,
    [string]$MigrationsDirectory,
    [string]$BackupFile
)

$ErrorActionPreference = 'Stop'
$script:failed = $false

. (Join-Path $PSScriptRoot '_MigrationFacts.ps1')
. (Join-Path $PSScriptRoot '_BackupMetadata.ps1')
. (Join-Path $PSScriptRoot '_BackupValidation.ps1')

function Fail([string]$Message) {
    Write-Error $Message
    exit 1
}

if ($RestoredDatabase -notmatch '^pjsk_restore_test_[a-z0-9_]+$') {
    Fail "RestoredDatabase must be a pjsk_restore_test_* database."
}
if ($SourceDatabase -and $SourceDatabase -notmatch '^pjsk_backup_source_test_[a-z0-9_]+$') {
    Fail "SourceDatabase must be an isolated pjsk_backup_source_test_* database."
}

$psql = Join-Path $PostgresBin 'psql.exe'
if (-not (Test-Path -LiteralPath $psql)) {
    Fail "PostgreSQL tool not found: $psql"
}

# Expected migration state is derived from the repository's migration files, not
# a literal, so it stays correct when 0020 and later land. A missing/invalid
# migrations directory is a hard failure — never a fallback to a fixed number.
if (-not $MigrationsDirectory) {
    $MigrationsDirectory = Resolve-RepositoryMigrationsDirectory -ScriptDirectory $PSScriptRoot
}
try {
    $migrationFacts = Get-MigrationFacts -MigrationsDirectory $MigrationsDirectory
} catch {
    Fail "Could not determine expected migrations from ${MigrationsDirectory}: $($_.Exception.Message)"
}

# --- optional backup binding: only when -BackupFile is supplied do we publish a
# .validation.json, the record that lets retention treat this backup as verified.
$publishValidation = $false
if ($BackupFile) {
    if (-not (Test-Path -LiteralPath $BackupFile -PathType Leaf)) {
        Fail "Backup file not found: $BackupFile"
    }
    if ([System.IO.Path]::GetExtension($BackupFile) -ne '.dump') {
        Fail "BackupFile must have the .dump extension."
    }
    $backupFull = [System.IO.Path]::GetFullPath($BackupFile)
    $backupDirectory = [System.IO.Path]::GetDirectoryName($backupFull)
    $backupBaseName = [System.IO.Path]::GetFileNameWithoutExtension($backupFull)
    # Retention anchors a set on <base>.metadata.json and derives
    # <base>.dump / <base>.validation.json beside it, so all three must share a
    # directory and a base name.
    $metadataPath = Join-Path $backupDirectory ($backupBaseName + '.metadata.json')
    $validationPath = Join-Path $backupDirectory ($backupBaseName + '.validation.json')
    $failureReportPath = Join-Path $backupDirectory ($backupBaseName + '.validation-failed.json')

    if (-not (Test-Path -LiteralPath $metadataPath -PathType Leaf)) {
        Fail "Metadata not found next to the dump: $metadataPath"
    }
    if (Test-Path -LiteralPath $validationPath) {
        Fail "A validation already exists and will not be overwritten: $validationPath"
    }

    $backupMetadata = $null
    try {
        $backupMetadata = Get-Content -LiteralPath $metadataPath -Raw -ErrorAction Stop | ConvertFrom-Json -ErrorAction Stop
    } catch {
        Fail "Metadata is not valid JSON: $metadataPath"
    }

    $flagReason = ''
    $backupIsolated = Get-MetadataIsolatedTestFlag -Metadata $backupMetadata -Reason ([ref]$flagReason)
    if ($null -eq $backupIsolated) {
        Fail "Metadata cannot be trusted: $flagReason"
    }

    $metadataSha = "$($backupMetadata.dumpSha256)"
    $actualSha = (Get-FileHash -LiteralPath $backupFull -Algorithm SHA256).Hash
    if (-not $metadataSha -or ($metadataSha -ine $actualSha)) {
        Fail "Dump SHA-256 does not match its metadata; refusing to validate a dump that changed since it was written."
    }
    $actualSize = (Get-Item -LiteralPath $backupFull).Length
    if ([long]$backupMetadata.dumpSizeBytes -ne [long]$actualSize) {
        Fail "Dump size does not match its metadata."
    }
    $publishValidation = $true
}

$connectionArguments = @('--host', $HostName, '--port', "$Port", '-w')
if ($Username) { $connectionArguments += @('--username', $Username) }

function Invoke-Scalar([string]$Database, [string]$Query) {
    $result = & $psql @connectionArguments '--dbname', $Database, '-X', '-A', '-t', '-c', $Query
    if ($LASTEXITCODE -ne 0) {
        Fail "query failed against $Database"
    }
    return "$result".Trim()
}

$keyTables = @(
    'schema_migrations', 'users', 'orders', 'order_items', 'payments',
    'payment_items', 'admins', 'admin_sessions', 'query_sessions',
    'user_recovery_emails', 'recovery_email_verification_codes',
    'query_code_recovery_codes', 'query_code_recovery_request_events',
    'query_code_recovery_sessions', 'user_query_code_bind_tokens',
    'account_security_audit_logs'
)

# Fixed catalog queries; table names come from the fixed list above and are
# passed through quote_literal-safe formatting only for to_regclass.
function Get-DatabaseProfile([string]$Database) {
    $profile = [ordered]@{}
    $profile.migrationMax = Invoke-Scalar $Database "select coalesce(max(version), '(none)') from schema_migrations"
    $profile.migrationCount = Invoke-Scalar $Database "select count(*) from schema_migrations"
    $profile.tableCount = Invoke-Scalar $Database "select count(*) from information_schema.tables where table_schema = 'public' and table_type = 'BASE TABLE'"
    $profile.primaryKeys = Invoke-Scalar $Database "select count(*) from pg_constraint c join pg_class r on r.oid = c.conrelid join pg_namespace n on n.oid = r.relnamespace where n.nspname = 'public' and c.contype = 'p'"
    $profile.foreignKeys = Invoke-Scalar $Database "select count(*) from pg_constraint c join pg_class r on r.oid = c.conrelid join pg_namespace n on n.oid = r.relnamespace where n.nspname = 'public' and c.contype = 'f'"
    $profile.indexes = Invoke-Scalar $Database "select count(*) from pg_indexes where schemaname = 'public'"
    $profile.sequences = Invoke-Scalar $Database "select count(*) from information_schema.sequences where sequence_schema = 'public'"
    $profile.pgcrypto = Invoke-Scalar $Database "select case when exists(select 1 from pg_extension where extname = 'pgcrypto') then 1 else 0 end"
    $profile.uuidFunction = Invoke-Scalar $Database "select case when gen_random_uuid() is not null then 1 else 0 end"
    $profile.rowCounts = [ordered]@{}
    foreach ($table in $keyTables) {
        $exists = Invoke-Scalar $Database "select case when to_regclass('public.$table') is null then 0 else 1 end"
        if ($exists -ne '1') {
            $profile.rowCounts[$table] = 'MISSING'
        } else {
            $profile.rowCounts[$table] = Invoke-Scalar $Database "select count(*) from public.$table"
        }
    }
    return $profile
}

function Assert([bool]$Condition, [string]$Description) {
    if ($Condition) {
        Write-Output "PASS  $Description"
    } else {
        Write-Output "FAIL  $Description"
        $script:failed = $true
    }
}

Write-Output "== restored database: $RestoredDatabase =="
$restored = Get-DatabaseProfile $RestoredDatabase
Write-Output ("  migration max     : " + $restored.migrationMax)
Write-Output ("  migration entries : " + $restored.migrationCount)
Write-Output ("  tables            : " + $restored.tableCount)
Write-Output ("  primary keys      : " + $restored.primaryKeys)
Write-Output ("  foreign keys      : " + $restored.foreignKeys)
Write-Output ("  indexes           : " + $restored.indexes)
Write-Output ("  sequences         : " + $restored.sequences)
Write-Output ("  pgcrypto          : " + $restored.pgcrypto)
Write-Output ("  UUID function     : " + $restored.uuidFunction)
foreach ($table in $keyTables) {
    Write-Output ("  rows {0,-40} : {1}" -f $table, $restored.rowCounts[$table])
}

Assert ($restored.migrationMax -eq $migrationFacts.MaxVersion) "maximum migration is $($migrationFacts.MaxVersion) (from $($migrationFacts.Count) repository migration files)"
Assert ([int]$restored.migrationCount -eq [int]$migrationFacts.Count) "schema_migrations has the expected $($migrationFacts.Count) entries"
foreach ($table in $keyTables) {
    Assert ($restored.rowCounts[$table] -ne 'MISSING') "table $table exists"
}
Assert ([int]$restored.primaryKeys -gt 0) "primary keys present"
Assert ([int]$restored.foreignKeys -gt 0) "foreign keys present"
Assert ([int]$restored.indexes -gt 0) "indexes present"
Assert ([int]$restored.sequences -eq 0) "sequence count is the expected zero"
Assert ($restored.pgcrypto -eq '1') "pgcrypto extension exists"
Assert ($restored.uuidFunction -eq '1') "gen_random_uuid() is callable"
Assert ([int]$restored.tableCount -ge $keyTables.Count) "table count is plausible"

if ($SourceDatabase) {
    Write-Output "== comparing against source: $SourceDatabase =="
    $source = Get-DatabaseProfile $SourceDatabase
    Assert ($source.migrationMax -eq $restored.migrationMax) "migration max version matches ($($source.migrationMax))"
    Assert ($source.migrationCount -eq $restored.migrationCount) "migration entry count matches"
    Assert ($source.tableCount -eq $restored.tableCount) "table count matches ($($source.tableCount))"
    Assert ($source.primaryKeys -eq $restored.primaryKeys) "primary key count matches ($($source.primaryKeys))"
    Assert ($source.foreignKeys -eq $restored.foreignKeys) "foreign key count matches ($($source.foreignKeys))"
    Assert ($source.indexes -eq $restored.indexes) "index count matches ($($source.indexes))"
    Assert ($source.sequences -eq $restored.sequences) "sequence count matches ($($source.sequences))"
    Assert ($source.pgcrypto -eq $restored.pgcrypto) "pgcrypto extension state matches"
    Assert ($source.uuidFunction -eq $restored.uuidFunction) "UUID function availability matches"
    foreach ($table in $keyTables) {
        Assert ($source.rowCounts[$table] -eq $restored.rowCounts[$table]) ("row count matches for {0} ({1})" -f $table, $source.rowCounts[$table])
    }
}

if ($script:failed) {
    # A failed drill must never publish a .validation.json. The failure report
    # uses a name retention does not read, so the set stays unverified (and
    # therefore Protected) while still leaving durable evidence of the failure.
    if ($publishValidation) {
        Write-BackupValidationFailureReport `
            -ReportPath $failureReportPath `
            -BackupFileName ([System.IO.Path]::GetFileName($backupFull)) `
            -RestoreDatabaseName $RestoredDatabase `
            -Summary 'One or more structural checks failed; see the console output of this run.'
        Write-Output "  validation      : NOT published (checks failed)"
        Write-Output "  failure report  : $failureReportPath"
    }
    Write-Output "RESULT: FAILED"
    exit 1
}

if ($publishValidation) {
    $validatorVersion = (& $psql '--version') -join ' '
    $record = New-BackupValidationRecord `
        -BackupFileName ([System.IO.Path]::GetFileName($backupFull)) `
        -MetadataFileName ([System.IO.Path]::GetFileName($metadataPath)) `
        -DumpSha256 $actualSha `
        -DumpSizeBytes ([long]$actualSize) `
        -RestoreDatabaseName $RestoredDatabase `
        -ValidatorVersion $validatorVersion `
        -MigrationCount ([int]$restored.migrationCount) `
        -MigrationMax "$($restored.migrationMax)" `
        -IsolatedTestBackup $backupIsolated

    $publishReason = ''
    $published = Publish-BackupValidation `
        -Record $record `
        -ValidationPath $validationPath `
        -ExpectedBackupFileName ([System.IO.Path]::GetFileName($backupFull)) `
        -ExpectedMetadataFileName ([System.IO.Path]::GetFileName($metadataPath)) `
        -ExpectedSha256 $actualSha `
        -ExpectedSizeBytes ([long]$actualSize) `
        -ExpectedIsolatedTestBackup $backupIsolated `
        -Reason ([ref]$publishReason)
    if (-not $published) {
        Write-Output "RESULT: FAILED"
        Fail "Could not publish the validation record: $publishReason"
    }
    Write-Output "  validation      : $validationPath"
}

Write-Output "RESULT: PASSED"
exit 0
