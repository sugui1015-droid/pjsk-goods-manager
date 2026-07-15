# Get-PostgresBackupRetentionReport.ps1 — READ-ONLY scan of a PostgreSQL
# backup root. Classifies each backup set and marks a retention decision.
# It NEVER deletes, moves, or rewrites any file. The scan/guard logic lives
# in _RetentionCommon.ps1 (param-less, safe to dot-source) so the cleanup
# script reuses the exact same judgement.
#
# Passwords / DSNs / environment values are never read or emitted.
[CmdletBinding()]
param(
    [Parameter(Mandatory)][string]$BackupRoot,
    [datetime]$Now = (Get-Date).ToUniversalTime(),
    [int]$KeepAllDays = 7,
    [int]$DailyDays = 30,
    [int]$WeeklyWeeks = 8,
    [int]$MonthlyMonths = 12,
    [int]$MinimumVerifiedBackups = 3,
    [int]$RecentChangeMinutes = 15,
    [string[]]$ProtectName = @(),
    [switch]$VerifyHash,
    [string]$OutputJson,
    [string]$OutputCsv,
    [switch]$AsObjects
)

$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot '_RetentionCommon.ps1')

$report = Get-PostgresBackupRetentionReport -BackupRoot $BackupRoot -Now $Now `
    -KeepAllDays $KeepAllDays -DailyDays $DailyDays -WeeklyWeeks $WeeklyWeeks `
    -MonthlyMonths $MonthlyMonths -MinimumVerifiedBackups $MinimumVerifiedBackups `
    -RecentChangeMinutes $RecentChangeMinutes -ProtectName $ProtectName -VerifyHash:$VerifyHash

if ($OutputJson) { $report | ConvertTo-Json -Depth 5 | Out-File -LiteralPath $OutputJson -Encoding utf8 }
if ($OutputCsv) { $report | Select-Object SetId, Status, Decision, RetentionTier, CreatedUtc, DumpSizeBytes, HasPartial, IsolatedTest, DecisionReason, Error | Export-Csv -LiteralPath $OutputCsv -NoTypeInformation -Encoding utf8 }

if ($AsObjects) { return $report }

$summary = $report | Group-Object Decision | ForEach-Object { "$($_.Name)=$($_.Count)" }
Write-Output ("Scanned {0} backup set(s): {1}" -f $report.Count, ($summary -join ' '))
$report | Select-Object SetId, Status, Decision, RetentionTier, CreatedUtc, DecisionReason | Format-Table -AutoSize | Out-String | Write-Output
