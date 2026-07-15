# Remove-ExpiredPostgresBackups.ps1 — DryRun-by-default cleanup of expired
# PostgreSQL backups. It REUSES Get-PostgresBackupRetentionReport (no second,
# looser judgement). It only ever touches sets the report marks 'Candidate',
# and only after a strict multi-factor confirmation plus an immediate re-scan.
#
# Real deletion requires ALL of:
#   -Execute  AND  -DryRun:$false  AND  -BackupRoot (guarded)
#   AND -ExpectedRootName matching the leaf directory
#   AND -ConfirmationText "DELETE_EXPIRED_PJSK_BACKUPS"
#   AND a re-scan with no Error decisions and the candidate still a Candidate.
#
# It never deletes isolated restore-drill evidence, unknown/incomplete/orphan
# sets, report files, or unknown directories. Passwords/DSNs are never touched.
[CmdletBinding()]
param(
    [Parameter(Mandatory)][string]$BackupRoot,
    [string]$ExpectedRootName,
    [string]$ConfirmationText,
    [datetime]$Now = (Get-Date).ToUniversalTime(),
    [int]$KeepAllDays = 7,
    [int]$DailyDays = 30,
    [int]$WeeklyWeeks = 8,
    [int]$MonthlyMonths = 12,
    [int]$MinimumVerifiedBackups = 3,
    [int]$RecentChangeMinutes = 15,
    [string[]]$ProtectName = @(),
    [switch]$VerifyHash,
    [switch]$Execute,
    [switch]$DryRun,
    [string]$OutputJson
)

$ErrorActionPreference = 'Stop'
$requiredConfirmation = 'DELETE_EXPIRED_PJSK_BACKUPS'

function Fail([string]$Message) { [Console]::Error.WriteLine("ERROR: $Message"); exit 2 }

# Reuse the exact report + guard logic (param-less common file, no clobber).
. (Join-Path $PSScriptRoot '_RetentionCommon.ps1')

$reportArgs = @{
    BackupRoot             = $BackupRoot
    Now                    = $Now
    KeepAllDays            = $KeepAllDays
    DailyDays              = $DailyDays
    WeeklyWeeks            = $WeeklyWeeks
    MonthlyMonths          = $MonthlyMonths
    MinimumVerifiedBackups = $MinimumVerifiedBackups
    RecentChangeMinutes    = $RecentChangeMinutes
    ProtectName            = $ProtectName
    VerifyHash             = $VerifyHash
}

$report = Get-PostgresBackupRetentionReport @reportArgs
$candidates = @($report | Where-Object { $_.Decision -eq 'Candidate' })
$errors = @($report | Where-Object { $_.Decision -eq 'Error' })

Write-Output ("Backup root : {0}" -f $BackupRoot)
Write-Output ("Sets total  : {0}" -f $report.Count)
Write-Output ("Candidates  : {0}" -f $candidates.Count)
Write-Output ("Errors      : {0}" -f $errors.Count)

$audit = @()
foreach ($c in $candidates) {
    $audit += [pscustomobject]@{ SetId = $c.SetId; Directory = $c.Directory; PlannedFiles = @($c.DumpPath, $c.MetadataPath, $c.ValidationPath | Where-Object { $_ }); Action = 'plan-delete'; Result = 'planned' }
}

# --- decide whether real execution is permitted ---
# Default is dry run. Real deletion requires -Execute and NOT -DryRun (an
# explicit -DryRun always forces a dry run). -DryRun is a switch, not a bool,
# so the interface binds cleanly under `powershell -File`.
$wantExecute = $Execute.IsPresent -and (-not $DryRun.IsPresent)
if (-not $wantExecute) {
    Write-Output "MODE: DryRun (no files will be deleted)."
    foreach ($c in $candidates) { Write-Output ("  would delete set: {0}  ({1})" -f $c.SetId, $c.DecisionReason) }
    if ($OutputJson) { $audit | ConvertTo-Json -Depth 5 | Out-File -LiteralPath $OutputJson -Encoding utf8 }
    exit 0
}

# --- gate the execution behind every confirmation factor ---
$reason = [ref]''
if (-not (Test-BackupRootGuard -Path $BackupRoot -Reason $reason)) { Fail "BackupRoot rejected: $($reason.Value)" }
$rootFull = $reason.Value
$leaf = Split-Path -Leaf $rootFull
if ([string]::IsNullOrWhiteSpace($ExpectedRootName)) { Fail "Execution requires -ExpectedRootName." }
if ($ExpectedRootName -cne $leaf) { Fail "ExpectedRootName '$ExpectedRootName' does not match the backup root leaf '$leaf'." }
if ($ConfirmationText -cne $requiredConfirmation) { Fail "ConfirmationText does not match the required phrase." }
if ($errors.Count -gt 0) { Fail "Report contains $($errors.Count) Error-level set(s); refusing to execute." }
if ($candidates.Count -eq 0) { Write-Output "No candidates to delete."; if ($OutputJson) { $audit | ConvertTo-Json -Depth 5 | Out-File -LiteralPath $OutputJson -Encoding utf8 }; exit 0 }

# --- immediate re-scan; only act on sets still Candidate and unchanged ---
$reReport = Get-PostgresBackupRetentionReport @reportArgs
$reBySet = @{}
foreach ($s in $reReport) { $reBySet[$s.SetId] = $s }

$rootPrefix = $rootFull.TrimEnd('\') + '\'
$failary = $false
$auditBySet = @{}
foreach ($a in $audit) { $auditBySet[$a.SetId] = $a }

foreach ($c in $candidates) {
    $entry = $auditBySet[$c.SetId]
    $re = $reBySet[$c.SetId]
    if (-not $re -or $re.Decision -ne 'Candidate') { $entry.Action = 'skip'; $entry.Result = 're-scan no longer a candidate'; continue }
    if ($re.HasPartial) { $entry.Action = 'skip'; $entry.Result = 'partial appeared on re-scan'; continue }
    if ("$($re.MetadataSha256)" -ine "$($c.MetadataSha256)") { $entry.Action = 'skip'; $entry.Result = 'metadata sha changed'; continue }
    if ("$($re.ValidationResult)" -ne "$($c.ValidationResult)") { $entry.Action = 'skip'; $entry.Result = 'validation status changed'; continue }
    if ($VerifyHash -and "$($re.ActualSha256)" -ine "$($c.ActualSha256)") { $entry.Action = 'skip'; $entry.Result = 'actual sha changed'; continue }

    # Delete only the identified set files, each re-checked to be under the root.
    $deleted = @()
    $ok = $true
    foreach ($f in @($c.DumpPath, $c.MetadataPath, $c.ValidationPath | Where-Object { $_ })) {
        try {
            $full = [System.IO.Path]::GetFullPath($f)
            if (-not (($full + '\').StartsWith($rootPrefix, [System.StringComparison]::OrdinalIgnoreCase)) -and
                -not ($full.StartsWith($rootPrefix, [System.StringComparison]::OrdinalIgnoreCase))) {
                throw "file escapes backup root"
            }
            if (Test-Path -LiteralPath $full -PathType Leaf) {
                Remove-Item -LiteralPath $full -Force -Confirm:$false
                $deleted += $full
            }
        } catch {
            $ok = $false; $failary = $true
            $entry.Action = 'delete'; $entry.Result = "FAILED: $($_.Exception.Message)"
        }
    }
    if ($ok) {
        $entry.Action = 'delete'; $entry.Result = 'deleted'
        $entry | Add-Member -NotePropertyName DeletedFiles -NotePropertyValue $deleted -Force
        # Remove the containing directory only if now empty and under root (not the root itself).
        $dir = [System.IO.Path]::GetFullPath($c.Directory).TrimEnd('\')
        if ($dir -ne $rootFull.TrimEnd('\') -and (($dir + '\').StartsWith($rootPrefix, [System.StringComparison]::OrdinalIgnoreCase))) {
            try {
                if (-not (Get-ChildItem -LiteralPath $dir -Force -ErrorAction Stop)) {
                    Remove-Item -LiteralPath $dir -Force -Confirm:$false
                    $entry.Result = 'deleted (and empty dir removed)'
                }
            } catch { }
        }
        Write-Output ("  deleted set: {0}" -f $c.SetId)
    }
}

if ($OutputJson) { $audit | ConvertTo-Json -Depth 5 | Out-File -LiteralPath $OutputJson -Encoding utf8 }

if ($failary) { Write-Output "RESULT: completed with one or more failures."; exit 1 }
Write-Output "RESULT: cleanup complete."
exit 0
