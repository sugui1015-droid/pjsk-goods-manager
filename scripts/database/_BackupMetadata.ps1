# _BackupMetadata.ps1 — shared validation of a backup's metadata partial before
# it is atomically published.
#
# This file defines ONLY functions (no param block, no top-level actions), so it
# can be dot-sourced from any script without clobbering caller variables.
# Read-only: nothing here connects to a database, writes, moves, or deletes any
# file. It only inspects an already-deserialized metadata object.

# Validates that the metadata just written and read back describes exactly the
# backup this run produced. Every check is "round-tripped value == expected
# value" — including the backup mode.
#
# Mode handling is the point of this function: isolatedTestBackup records which
# kind of backup this is (a real backup of a production database, or an isolated
# restore-drill backup). It must MATCH this run's mode, not be true. Requiring it
# to be true made every real backup fail to publish, since a real backup
# correctly records false. See the 2026-07-15 development log.
#
# Returns $true when the metadata is consistent; otherwise $false with a short,
# non-sensitive reason in $Reason (field-level causes only — never hashes,
# paths, credentials, or DSNs).
function Test-BackupMetadataConsistency {
    [CmdletBinding()]
    param(
        [Parameter(Mandatory)][AllowNull()]$Metadata,
        [Parameter(Mandatory)][string]$ExpectedDatabaseName,
        [Parameter(Mandatory)][string]$ExpectedSha256,
        [Parameter(Mandatory)][long]$ExpectedSizeBytes,
        [Parameter(Mandatory)][bool]$ExpectedIsolatedTestBackup,
        [Parameter(Mandatory)][ref]$Reason
    )

    if ($null -eq $Metadata) {
        $Reason.Value = 'metadata is null'
        return $false
    }

    if ("$($Metadata.sourceDatabaseName)" -ne $ExpectedDatabaseName) {
        $Reason.Value = 'sourceDatabaseName does not match this run'
        return $false
    }
    if ("$($Metadata.dumpSha256)" -ne $ExpectedSha256) {
        $Reason.Value = 'dumpSha256 does not match the dump just written'
        return $false
    }
    if ([long]$Metadata.dumpSizeBytes -ne $ExpectedSizeBytes) {
        $Reason.Value = 'dumpSizeBytes does not match the dump just written'
        return $false
    }
    if ("$($Metadata.dumpFormat)" -ne 'custom') {
        $Reason.Value = 'dumpFormat is not custom'
        return $false
    }

    # isolatedTestBackup must be present AND a real boolean. A missing property
    # deserializes to $null and a string "false" casts to $true, so casting with
    # [bool] would silently accept both — exactly the confusion this guards.
    $isolatedProperty = $null
    if ($Metadata.PSObject -and $Metadata.PSObject.Properties) {
        $isolatedProperty = $Metadata.PSObject.Properties['isolatedTestBackup']
    }
    if (-not $isolatedProperty) {
        $Reason.Value = 'isolatedTestBackup is missing'
        return $false
    }
    if ($isolatedProperty.Value -isnot [bool]) {
        $Reason.Value = 'isolatedTestBackup is not a boolean'
        return $false
    }
    if ($isolatedProperty.Value -ne $ExpectedIsolatedTestBackup) {
        $Reason.Value = "isolatedTestBackup is $($isolatedProperty.Value) but this run expects $ExpectedIsolatedTestBackup"
        return $false
    }

    $Reason.Value = 'consistent'
    return $true
}
