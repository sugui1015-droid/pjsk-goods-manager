# _BackupValidation.ps1 — shared construction, checking, and atomic publication
# of a backup's .validation.json, the record that closes the
# backup -> restore-drill verification -> retention loop.
#
# This file defines ONLY functions (no param block, no top-level actions), so it
# can be dot-sourced from any script without clobbering caller variables. It
# never connects to a database and never runs a PostgreSQL tool.
#
# FIELD NAMES ARE NOT ARBITRARY. _RetentionCommon.ps1 reads exactly two fields to
# decide whether a backup counts as verified:
#   overallResult  — must be the literal string 'passed'
#   dumpSha256     — must equal the metadata's dumpSha256
# Renaming either (e.g. to a "status" field) silently leaves every backup
# 'unverified', so the retention tiers never engage. Keep them in sync with
# _RetentionCommon.ps1.

# The only value _RetentionCommon.ps1 accepts as a successful validation.
function Get-BackupValidationPassedResult {
    return 'passed'
}

# The closed set of validation purposes.
#
#   current       — the default. The restored backup must match the repository's
#                   migrations exactly. This is the normal case and must never
#                   be weakened.
#   pre-migration — an upgrade-time baseline: the backup was taken from a
#                   database that is deliberately BEHIND the repository (for
#                   example production at 0018 while the repository is at 0019),
#                   captured as a rollback point before the migration runs.
#                   Only ever selectable by passing an explicit expected
#                   migration set; never inferred, never a fallback.
function Get-BackupValidationPurposes {
    return @('current', 'pre-migration')
}

# Reads an expected migration set file: one migration file name per line.
#
# The set is deliberately supplied from OUTSIDE this script (exported read-only
# from the database being backed up) so validation cannot be satisfied by
# deriving expectations from the very database it is checking.
#
# Rejects anything that would make the comparison meaningless: a missing file,
# an empty set, duplicates, a name that is not a well-formed migration file
# name, or a set that is not in strict ascending ordinal order.
function Read-ExpectedMigrationSetFile {
    [CmdletBinding()]
    param(
        [Parameter(Mandatory)][string]$Path,
        [Parameter(Mandatory)][ref]$Reason
    )

    if ([string]::IsNullOrWhiteSpace($Path)) {
        $Reason.Value = 'expected migration set file path is empty'
        return $null
    }
    if (-not (Test-Path -LiteralPath $Path -PathType Leaf)) {
        $Reason.Value = "expected migration set file not found: $([System.IO.Path]::GetFileName($Path))"
        return $null
    }

    $lines = @(Get-Content -LiteralPath $Path -ErrorAction Stop |
        ForEach-Object { "$_".Trim() } |
        Where-Object { $_ -ne '' })

    if ($lines.Count -eq 0) {
        $Reason.Value = 'expected migration set file is empty'
        return $null
    }

    $pattern = Get-MigrationFileNamePattern
    foreach ($line in $lines) {
        if ($line -notmatch $pattern) {
            $Reason.Value = "expected migration set contains an invalid migration file name: $line"
            return $null
        }
    }

    $seen = @{}
    foreach ($line in $lines) {
        if ($seen.ContainsKey($line)) {
            $Reason.Value = "expected migration set contains a duplicate: $line"
            return $null
        }
        $seen[$line] = $true
    }

    # Strict ascending ordinal order, matching the migration runner's own sort.
    $sorted = [string[]]$lines
    [Array]::Sort($sorted, [System.StringComparer]::Ordinal)
    for ($i = 0; $i -lt $lines.Count; $i++) {
        if ($lines[$i] -cne $sorted[$i]) {
            $Reason.Value = 'expected migration set is not in strict ascending order'
            return $null
        }
    }

    $Reason.Value = 'ok'
    return [ordered]@{
        Names      = $sorted
        Count      = $sorted.Count
        MaxVersion = $sorted[$sorted.Count - 1]
        Sha256     = (Get-FileHash -LiteralPath $Path -Algorithm SHA256).Hash
    }
}

# Builds a validation record. By construction this only ever produces a PASSED
# record — callers must not invoke it unless every check actually passed. A
# failed drill is recorded separately under a different file name (see
# Write-BackupValidationFailureReport) so retention can never read it as success.
#
# Deliberately excluded, to avoid leaking anything useful to an attacker or
# anything retention does not need: host, port, username, PGPASSWORD/PGPASSFILE,
# command lines, DSNs, absolute paths (file NAMES only), and business row counts.
function New-BackupValidationRecord {
    [CmdletBinding()]
    param(
        [Parameter(Mandatory)][string]$BackupFileName,
        [Parameter(Mandatory)][string]$MetadataFileName,
        [Parameter(Mandatory)][string]$DumpSha256,
        [Parameter(Mandatory)][long]$DumpSizeBytes,
        [Parameter(Mandatory)][string]$RestoreDatabaseName,
        [Parameter(Mandatory)][string]$ValidatorVersion,
        [Parameter(Mandatory)][int]$MigrationCount,
        [Parameter(Mandatory)][string]$MigrationMax,
        [Parameter(Mandatory)][bool]$IsolatedTestBackup,
        [string]$ValidationPurpose = 'current',
        [int]$ExpectedMigrationCount = 0,
        [string]$ExpectedMigrationMax = '',
        [string]$ExpectedMigrationSetSha256 = '',
        [datetime]$ValidatedUtc = ([datetime]::UtcNow)
    )

    if ($ValidationPurpose -notin (Get-BackupValidationPurposes)) {
        throw "ValidationPurpose must be one of: $((Get-BackupValidationPurposes) -join ', ')"
    }
    if ($ExpectedMigrationCount -le 0) { $ExpectedMigrationCount = $MigrationCount }
    if ([string]::IsNullOrWhiteSpace($ExpectedMigrationMax)) { $ExpectedMigrationMax = $MigrationMax }

    $record = [ordered]@{
        schemaVersion          = 1
        overallResult          = (Get-BackupValidationPassedResult)
        validationPurpose      = $ValidationPurpose
        backupFileName         = $BackupFileName
        metadataFileName       = $MetadataFileName
        dumpSha256             = $DumpSha256
        dumpSizeBytes          = $DumpSizeBytes
        validatedUtc           = $ValidatedUtc.ToUniversalTime().ToString('o')
        restoreDatabaseName    = $RestoreDatabaseName
        validatorVersion       = $ValidatorVersion
        migrationCount         = $MigrationCount
        migrationMax           = $MigrationMax
        expectedMigrationCount = $ExpectedMigrationCount
        expectedMigrationMax   = $ExpectedMigrationMax
        isolatedTestBackup     = $IsolatedTestBackup
    }
    if ($ExpectedMigrationSetSha256) {
        $record.expectedMigrationSetSha256 = $ExpectedMigrationSetSha256
    }
    return $record
}

# Validates a validation record that has been read back from disk against what
# this run intended to write. Mirrors Test-BackupMetadataConsistency: every check
# is "round-tripped value == expected value", plus strict type checks so a JSON
# round trip cannot quietly change a number into a string.
function Test-BackupValidationConsistency {
    [CmdletBinding()]
    param(
        [Parameter(Mandatory)][AllowNull()]$Validation,
        [Parameter(Mandatory)][string]$ExpectedBackupFileName,
        [Parameter(Mandatory)][string]$ExpectedMetadataFileName,
        [Parameter(Mandatory)][string]$ExpectedSha256,
        [Parameter(Mandatory)][long]$ExpectedSizeBytes,
        [Parameter(Mandatory)][bool]$ExpectedIsolatedTestBackup,
        [Parameter(Mandatory)][ref]$Reason
    )

    if ($null -eq $Validation) {
        $Reason.Value = 'validation is null'
        return $false
    }

    # status is a closed enum, not free text.
    if ("$($Validation.overallResult)" -ne (Get-BackupValidationPassedResult)) {
        $Reason.Value = 'overallResult is not the accepted passed value'
        return $false
    }
    if ("$($Validation.backupFileName)" -ne $ExpectedBackupFileName) {
        $Reason.Value = 'backupFileName does not match the dump being validated'
        return $false
    }
    if ("$($Validation.metadataFileName)" -ne $ExpectedMetadataFileName) {
        $Reason.Value = 'metadataFileName does not match the metadata being validated'
        return $false
    }
    if ("$($Validation.dumpSha256)" -ne $ExpectedSha256) {
        $Reason.Value = 'dumpSha256 does not match the dump on disk'
        return $false
    }
    if ($Validation.schemaVersion -isnot [int] -or [int]$Validation.schemaVersion -ne 1) {
        $Reason.Value = 'schemaVersion is not the number 1'
        return $false
    }
    if (-not ($Validation.dumpSizeBytes -is [int] -or $Validation.dumpSizeBytes -is [long])) {
        $Reason.Value = 'dumpSizeBytes is not a number'
        return $false
    }
    if ([long]$Validation.dumpSizeBytes -ne $ExpectedSizeBytes) {
        $Reason.Value = 'dumpSizeBytes does not match the dump on disk'
        return $false
    }
    if (-not ($Validation.migrationCount -is [int] -or $Validation.migrationCount -is [long])) {
        $Reason.Value = 'migrationCount is not a number'
        return $false
    }
    # validationPurpose is a closed enum, not free text.
    if ("$($Validation.validationPurpose)" -notin (Get-BackupValidationPurposes)) {
        $Reason.Value = 'validationPurpose is not one of the accepted values'
        return $false
    }
    if (-not ($Validation.expectedMigrationCount -is [int] -or $Validation.expectedMigrationCount -is [long])) {
        $Reason.Value = 'expectedMigrationCount is not a number'
        return $false
    }
    if ([long]$Validation.expectedMigrationCount -ne [long]$Validation.migrationCount) {
        $Reason.Value = 'expectedMigrationCount does not match the migrationCount that was verified'
        return $false
    }
    if ("$($Validation.expectedMigrationMax)" -ne "$($Validation.migrationMax)") {
        $Reason.Value = 'expectedMigrationMax does not match the migrationMax that was verified'
        return $false
    }

    $flagReason = ''
    $isolated = Get-MetadataIsolatedTestFlag -Metadata $Validation -Reason ([ref]$flagReason)
    if ($null -eq $isolated) {
        $Reason.Value = "validation $flagReason"
        return $false
    }
    if ($isolated -ne $ExpectedIsolatedTestBackup) {
        $Reason.Value = 'isolatedTestBackup does not match the metadata being validated'
        return $false
    }

    $Reason.Value = 'consistent'
    return $true
}

# Atomically publishes a validation record: write .partial -> read back ->
# verify -> rename. Refuses to overwrite an existing validation. On any failure
# the partial is removed and $false is returned; the dump, the metadata, and any
# pre-existing validation are never touched.
function Publish-BackupValidation {
    [CmdletBinding()]
    param(
        [Parameter(Mandatory)]$Record,
        [Parameter(Mandatory)][string]$ValidationPath,
        [Parameter(Mandatory)][string]$ExpectedBackupFileName,
        [Parameter(Mandatory)][string]$ExpectedMetadataFileName,
        [Parameter(Mandatory)][string]$ExpectedSha256,
        [Parameter(Mandatory)][long]$ExpectedSizeBytes,
        [Parameter(Mandatory)][bool]$ExpectedIsolatedTestBackup,
        [Parameter(Mandatory)][ref]$Reason
    )

    $partialPath = $ValidationPath + '.partial'

    if (Test-Path -LiteralPath $ValidationPath) {
        $Reason.Value = 'a validation already exists; refusing to overwrite it'
        return $false
    }
    if (Test-Path -LiteralPath $partialPath) {
        $Reason.Value = 'a validation partial already exists; refusing to overwrite it'
        return $false
    }

    try {
        $Record | ConvertTo-Json | Out-File -LiteralPath $partialPath -Encoding utf8
        $readBack = Get-Content -LiteralPath $partialPath -Raw | ConvertFrom-Json

        $checkReason = ''
        $consistent = Test-BackupValidationConsistency `
            -Validation $readBack `
            -ExpectedBackupFileName $ExpectedBackupFileName `
            -ExpectedMetadataFileName $ExpectedMetadataFileName `
            -ExpectedSha256 $ExpectedSha256 `
            -ExpectedSizeBytes $ExpectedSizeBytes `
            -ExpectedIsolatedTestBackup $ExpectedIsolatedTestBackup `
            -Reason ([ref]$checkReason)
        if (-not $consistent) {
            throw "validation partial check failed: $checkReason"
        }

        Move-Item -LiteralPath $partialPath -Destination $ValidationPath
    } catch {
        if (Test-Path -LiteralPath $partialPath) {
            Remove-Item -LiteralPath $partialPath -Force -Confirm:$false
        }
        $Reason.Value = "$($_.Exception.Message)"
        return $false
    }

    $Reason.Value = 'published'
    return $true
}

# Records a FAILED drill under a deliberately different name.
#
# _RetentionCommon.ps1 only ever looks for '*.validation.json'; this name does
# not end with that, so retention ignores the file entirely and the backup set
# stays 'unverified' -> Protected. That keeps a durable record of why a drill
# failed with no way for it to be mistaken for success.
function Write-BackupValidationFailureReport {
    [CmdletBinding()]
    param(
        [Parameter(Mandatory)][string]$ReportPath,
        [Parameter(Mandatory)][string]$BackupFileName,
        [Parameter(Mandatory)][string]$RestoreDatabaseName,
        [Parameter(Mandatory)][string]$Summary,
        [datetime]$ValidatedUtc = ([datetime]::UtcNow)
    )

    $record = [ordered]@{
        schemaVersion       = 1
        overallResult       = 'failed'
        backupFileName      = $BackupFileName
        restoreDatabaseName = $RestoreDatabaseName
        validatedUtc        = $ValidatedUtc.ToUniversalTime().ToString('o')
        failureSummary      = $Summary
    }
    $record | ConvertTo-Json | Out-File -LiteralPath $ReportPath -Encoding utf8
}
