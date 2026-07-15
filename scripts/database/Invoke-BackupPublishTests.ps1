# Invoke-BackupPublishTests.ps1 — offline tests for the backup PUBLISH path of
# Backup-Postgres.ps1: the step that writes the metadata partial, validates it,
# and atomically promotes the dump + metadata to their final names.
#
# Why this suite exists: every other Backup-Postgres.ps1 test uses -DryRun,
# which exits before pg_dump ever runs — so the publish path had zero coverage,
# which is how a check that made every REAL backup fail to publish went
# unnoticed. See the 2026-07-15 development log.
#
# NO real PostgreSQL is involved. Minimal mock pg_dump.exe / pg_restore.exe are
# compiled with the .NET Framework C# compiler that ships with Windows into a
# throwaway directory, so the real script's real publish path executes verbatim.
# Never connects to a database; never reads or writes any real backup directory.
[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'
$scriptDir = $PSScriptRoot
$backup = Join-Path $scriptDir 'Backup-Postgres.ps1'
$script:fail = 0
$script:pass = 0

function Check([bool]$Condition, [string]$Name) {
    if ($Condition) { Write-Output "PASS  $Name"; $script:pass++ } else { Write-Output "FAIL  $Name"; $script:fail++ }
}

# Dot-source the param-less common file to unit-test the validator that the real
# publish path calls (used for the corruption cases the entry point cannot inject).
. (Join-Path $scriptDir '_BackupMetadata.ps1')

$testRoot = Join-Path ([System.IO.Path]::GetTempPath()) ('pjsk-publish-tests-' + [guid]::NewGuid().ToString('N').Substring(0, 10))
New-Item -ItemType Directory -Force -Path $testRoot | Out-Null

try {
    # ---------------------------------------------------------------------
    # Build minimal mock PostgreSQL tools (NOT real pg_dump / pg_restore).
    # ---------------------------------------------------------------------
    $csc = Join-Path $env:WINDIR 'Microsoft.NET\Framework64\v4.0.30319\csc.exe'
    if (-not (Test-Path -LiteralPath $csc)) {
        Write-Output "FAIL  prerequisite: C# compiler not found at $csc"
        exit 1
    }
    $mockBin = Join-Path $testRoot 'mock-bin'
    New-Item -ItemType Directory -Force -Path $mockBin | Out-Null

    # Mock pg_dump: honours --version, and writes a fake dump to --file.
    $dumpSource = Join-Path $testRoot 'mock_pg_dump.cs'
    Set-Content -LiteralPath $dumpSource -Encoding ascii -Value @'
using System;
using System.IO;
class MockPgDump {
    static int Main(string[] args) {
        for (int i = 0; i < args.Length; i++) {
            if (args[i] == "--version") { Console.WriteLine("pg_dump (PostgreSQL) 18.0 (MOCK - NOT REAL)"); return 0; }
        }
        for (int i = 0; i < args.Length; i++) {
            if (args[i] == "--file" && i + 1 < args.Length) {
                File.WriteAllText(args[i + 1], "MOCK CUSTOM FORMAT DUMP - NOT REAL POSTGRESQL DATA");
                return 0;
            }
        }
        return 1;
    }
}
'@
    # Mock pg_restore: --list prints archive-entry lines matching ^\d+;
    $restoreSource = Join-Path $testRoot 'mock_pg_restore.cs'
    Set-Content -LiteralPath $restoreSource -Encoding ascii -Value @'
using System;
class MockPgRestore {
    static int Main(string[] args) {
        Console.WriteLine("; Archive created (MOCK)");
        Console.WriteLine("1; 2345 6789 TABLE public users mock");
        Console.WriteLine("2; 2345 6790 TABLE public orders mock");
        return 0;
    }
}
'@
    & $csc '/nologo' '/target:exe' ("/out:" + (Join-Path $mockBin 'pg_dump.exe')) $dumpSource | Out-Null
    $cscDumpExit = $LASTEXITCODE
    & $csc '/nologo' '/target:exe' ("/out:" + (Join-Path $mockBin 'pg_restore.exe')) $restoreSource | Out-Null
    $cscRestoreExit = $LASTEXITCODE
    Check ($cscDumpExit -eq 0 -and $cscRestoreExit -eq 0 -and (Test-Path (Join-Path $mockBin 'pg_dump.exe')) -and (Test-Path (Join-Path $mockBin 'pg_restore.exe'))) "mock pg_dump/pg_restore built (no real PostgreSQL involved)"

    $powershell = (Get-Command powershell.exe).Source
    function Invoke-Backup {
        param([string[]]$Arguments)
        $previous = $ErrorActionPreference
        $ErrorActionPreference = 'Continue'
        $output = (& $powershell -NoProfile -NonInteractive -ExecutionPolicy Bypass -File $backup @Arguments 2>&1 | ForEach-Object { "$_" }) -join "`n"
        $code = $LASTEXITCODE
        $ErrorActionPreference = $previous
        return [pscustomobject]@{ Output = $output; ExitCode = $code }
    }

    # =====================================================================
    # 1. REAL production backup: no -RequireIsolatedSource, real DB name.
    #    This is the documented primary usage (docs/database-backup-restore.md)
    #    and the exact path that used to fail 100% of the time.
    # =====================================================================
    $realRoot = Join-Path $testRoot 'real-backup-root'
    New-Item -ItemType Directory -Force -Path $realRoot | Out-Null
    $realDump = Join-Path $realRoot 'pjsk-real.dump'
    $realMeta = Join-Path $realRoot 'pjsk-real.metadata.json'
    $result = Invoke-Backup @('-DatabaseName', 'pjsk', '-BackupRoot', $realRoot, '-OutputPath', $realDump, '-PostgresBin', $mockBin)

    Check ($result.ExitCode -eq 0) "real production backup (pjsk, no -RequireIsolatedSource) publishes successfully"
    Check (Test-Path -LiteralPath $realDump) "real production backup: final dump published"
    Check (Test-Path -LiteralPath $realMeta) "real production backup: final metadata published"
    Check (-not (Test-Path -LiteralPath ($realDump + '.partial'))) "real production backup: no dump .partial left behind"
    Check (-not (Test-Path -LiteralPath ($realMeta + '.partial'))) "real production backup: no metadata .partial left behind"

    $realMetaObj = $null
    if (Test-Path -LiteralPath $realMeta) { $realMetaObj = Get-Content -LiteralPath $realMeta -Raw | ConvertFrom-Json }
    Check ($realMetaObj -and $realMetaObj.isolatedTestBackup -eq $false) "real production backup: metadata records isolatedTestBackup = false"
    Check ($realMetaObj -and $realMetaObj.isolatedTestBackup -is [bool]) "real production backup: isolatedTestBackup is a real boolean"
    Check ($realMetaObj -and $realMetaObj.sourceDatabaseName -eq 'pjsk') "real production backup: metadata records the real database name"
    Check ($realMetaObj -and ($null -eq $realMetaObj.fixtureExpectedRowCounts)) "real production backup: no drill fixture row counts in metadata"
    # A real backup must never be mistaken for drill evidence by the retention tooling.
    Check ($realMetaObj -and $realMetaObj.isolatedTestBackup -ne $true) "real production backup: not marked as isolated drill evidence"

    # =====================================================================
    # 2. ISOLATED restore-drill backup: -RequireIsolatedSource + drill DB name.
    # =====================================================================
    $drillRoot = Join-Path $testRoot 'drill-backup-root'
    New-Item -ItemType Directory -Force -Path $drillRoot | Out-Null
    $drillDump = Join-Path $drillRoot 'pjsk-drill.dump'
    $drillMeta = Join-Path $drillRoot 'pjsk-drill.metadata.json'
    $result = Invoke-Backup @('-DatabaseName', 'pjsk_backup_source_test_x', '-BackupRoot', $drillRoot, '-OutputPath', $drillDump, '-PostgresBin', $mockBin, '-RequireIsolatedSource')

    Check ($result.ExitCode -eq 0) "isolated drill backup (-RequireIsolatedSource) publishes successfully"
    Check (Test-Path -LiteralPath $drillDump) "isolated drill backup: final dump published"
    Check (Test-Path -LiteralPath $drillMeta) "isolated drill backup: final metadata published"
    Check (-not (Test-Path -LiteralPath ($drillDump + '.partial'))) "isolated drill backup: no dump .partial left behind"

    $drillMetaObj = $null
    if (Test-Path -LiteralPath $drillMeta) { $drillMetaObj = Get-Content -LiteralPath $drillMeta -Raw | ConvertFrom-Json }
    Check ($drillMetaObj -and $drillMetaObj.isolatedTestBackup -eq $true) "isolated drill backup: metadata records isolatedTestBackup = true"
    Check ($drillMetaObj -and $null -ne $drillMetaObj.fixtureExpectedRowCounts) "isolated drill backup: metadata includes drill fixture row counts"
    Check ($drillMetaObj -and $drillMetaObj.fixtureExpectedRowCounts.schema_migrations -ge 19) "isolated drill backup: fixture schema_migrations count is dynamic (>= 19), not a stale literal"

    # The two modes must be distinguishable in the published metadata.
    Check ($realMetaObj -and $drillMetaObj -and ($realMetaObj.isolatedTestBackup -ne $drillMetaObj.isolatedTestBackup)) "real and drill backups are distinguishable by isolatedTestBackup"

    # =====================================================================
    # 3. The isolated-source gate still protects the real database.
    # =====================================================================
    $gateRoot = Join-Path $testRoot 'gate-root'
    New-Item -ItemType Directory -Force -Path $gateRoot | Out-Null
    $result = Invoke-Backup @('-DatabaseName', 'pjsk', '-BackupRoot', $gateRoot, '-PostgresBin', $mockBin, '-RequireIsolatedSource')
    Check ($result.ExitCode -ne 0) "drill mode still refuses the production database pjsk"
    Check (@(Get-ChildItem -Path $gateRoot -Recurse -File -ErrorAction SilentlyContinue).Count -eq 0) "refused drill-mode run produced no files at all"

    # =====================================================================
    # 4. Validator: mode mismatches must fail (the entry point cannot inject
    #    these, so they target the same function the publish path calls).
    # =====================================================================
    function New-Meta {
        param([string]$Db = 'pjsk', [string]$Sha = 'ABC123', [long]$Size = 42, $Isolated = $false, [switch]$OmitIsolated)
        $h = [ordered]@{ sourceDatabaseName = $Db; dumpSha256 = $Sha; dumpSizeBytes = $Size; dumpFormat = 'custom' }
        if (-not $OmitIsolated) { $h.isolatedTestBackup = $Isolated }
        # Round-trip through JSON so the test sees exactly what the script sees.
        return ($h | ConvertTo-Json | ConvertFrom-Json)
    }
    $reason = ''

    $ok = Test-BackupMetadataConsistency -Metadata (New-Meta -Isolated $false) -ExpectedDatabaseName 'pjsk' -ExpectedSha256 'ABC123' -ExpectedSizeBytes 42 -ExpectedIsolatedTestBackup $false -Reason ([ref]$reason)
    Check $ok "validator accepts real-mode metadata in real mode"

    $ok = Test-BackupMetadataConsistency -Metadata (New-Meta -Db 'pjsk_backup_source_test_x' -Isolated $true) -ExpectedDatabaseName 'pjsk_backup_source_test_x' -ExpectedSha256 'ABC123' -ExpectedSizeBytes 42 -ExpectedIsolatedTestBackup $true -Reason ([ref]$reason)
    Check $ok "validator accepts drill-mode metadata in drill mode"

    $ok = Test-BackupMetadataConsistency -Metadata (New-Meta -Isolated $true) -ExpectedDatabaseName 'pjsk' -ExpectedSha256 'ABC123' -ExpectedSizeBytes 42 -ExpectedIsolatedTestBackup $false -Reason ([ref]$reason)
    Check ((-not $ok) -and $reason -match 'isolatedTestBackup') "real mode rejects metadata claiming isolatedTestBackup = true"

    $ok = Test-BackupMetadataConsistency -Metadata (New-Meta -Isolated $false) -ExpectedDatabaseName 'pjsk' -ExpectedSha256 'ABC123' -ExpectedSizeBytes 42 -ExpectedIsolatedTestBackup $true -Reason ([ref]$reason)
    Check ((-not $ok) -and $reason -match 'isolatedTestBackup') "drill mode rejects metadata claiming isolatedTestBackup = false"

    $ok = Test-BackupMetadataConsistency -Metadata (New-Meta -OmitIsolated) -ExpectedDatabaseName 'pjsk' -ExpectedSha256 'ABC123' -ExpectedSizeBytes 42 -ExpectedIsolatedTestBackup $false -Reason ([ref]$reason)
    Check ((-not $ok) -and $reason -match 'missing') "a missing isolatedTestBackup field fails instead of defaulting to false"

    # "false" is a NON-EMPTY STRING, which [bool] casts to $true in PowerShell.
    $ok = Test-BackupMetadataConsistency -Metadata (New-Meta -Isolated 'false') -ExpectedDatabaseName 'pjsk' -ExpectedSha256 'ABC123' -ExpectedSizeBytes 42 -ExpectedIsolatedTestBackup $false -Reason ([ref]$reason)
    Check ((-not $ok) -and $reason -match 'not a boolean') 'the string "false" is rejected as not a boolean (never coerced to true)'

    $ok = Test-BackupMetadataConsistency -Metadata (New-Meta -Isolated 'true') -ExpectedDatabaseName 'pjsk' -ExpectedSha256 'ABC123' -ExpectedSizeBytes 42 -ExpectedIsolatedTestBackup $true -Reason ([ref]$reason)
    Check ((-not $ok) -and $reason -match 'not a boolean') 'the string "true" is rejected as not a boolean'

    $ok = Test-BackupMetadataConsistency -Metadata (New-Meta -Isolated 1) -ExpectedDatabaseName 'pjsk' -ExpectedSha256 'ABC123' -ExpectedSizeBytes 42 -ExpectedIsolatedTestBackup $true -Reason ([ref]$reason)
    Check ((-not $ok) -and $reason -match 'not a boolean') "the integer 1 is rejected as not a boolean"

    $ok = Test-BackupMetadataConsistency -Metadata $null -ExpectedDatabaseName 'pjsk' -ExpectedSha256 'ABC123' -ExpectedSizeBytes 42 -ExpectedIsolatedTestBackup $false -Reason ([ref]$reason)
    Check (-not $ok) "null metadata is rejected"

    # The pre-existing consistency checks must survive the fix.
    $ok = Test-BackupMetadataConsistency -Metadata (New-Meta) -ExpectedDatabaseName 'other_db' -ExpectedSha256 'ABC123' -ExpectedSizeBytes 42 -ExpectedIsolatedTestBackup $false -Reason ([ref]$reason)
    Check ((-not $ok) -and $reason -match 'sourceDatabaseName') "a mismatched database name still fails"
    $ok = Test-BackupMetadataConsistency -Metadata (New-Meta) -ExpectedDatabaseName 'pjsk' -ExpectedSha256 'DIFFERENT' -ExpectedSizeBytes 42 -ExpectedIsolatedTestBackup $false -Reason ([ref]$reason)
    Check ((-not $ok) -and $reason -match 'dumpSha256') "a mismatched SHA-256 still fails"
    $ok = Test-BackupMetadataConsistency -Metadata (New-Meta) -ExpectedDatabaseName 'pjsk' -ExpectedSha256 'ABC123' -ExpectedSizeBytes 999 -ExpectedIsolatedTestBackup $false -Reason ([ref]$reason)
    Check ((-not $ok) -and $reason -match 'dumpSizeBytes') "a mismatched dump size still fails"

    # =====================================================================
    # 5. Overwrite protection: an existing valid backup is never destroyed.
    # =====================================================================
    $keepRoot = Join-Path $testRoot 'keep-root'
    New-Item -ItemType Directory -Force -Path $keepRoot | Out-Null
    $keepDump = Join-Path $keepRoot 'pjsk-existing.dump'
    Set-Content -LiteralPath $keepDump -Value 'PRECIOUS EXISTING BACKUP' -Encoding ascii
    $keepBefore = (Get-FileHash -LiteralPath $keepDump -Algorithm SHA256).Hash
    $result = Invoke-Backup @('-DatabaseName', 'pjsk', '-BackupRoot', $keepRoot, '-OutputPath', $keepDump, '-PostgresBin', $mockBin)
    Check ($result.ExitCode -ne 0) "a run targeting an existing dump is refused"
    Check ((Test-Path -LiteralPath $keepDump) -and ((Get-FileHash -LiteralPath $keepDump -Algorithm SHA256).Hash -eq $keepBefore)) "the pre-existing backup is left untouched"

    # An unrelated earlier backup must survive a later successful run.
    $result = Invoke-Backup @('-DatabaseName', 'pjsk', '-BackupRoot', $keepRoot, '-OutputPath', (Join-Path $keepRoot 'pjsk-new.dump'), '-PostgresBin', $mockBin)
    Check ($result.ExitCode -eq 0) "a later run to a fresh name succeeds"
    Check ((Test-Path -LiteralPath $keepDump) -and ((Get-FileHash -LiteralPath $keepDump -Algorithm SHA256).Hash -eq $keepBefore)) "an unrelated existing backup survives a successful run"

    # =====================================================================
    # 6. Secrets must never appear in output or metadata.
    # =====================================================================
    Check ($result.Output -notmatch '(?i)PGPASSWORD') "backup output never mentions PGPASSWORD"
    $realMetaRaw = Get-Content -LiteralPath $realMeta -Raw
    Check ($realMetaRaw -notmatch '(?i)(password|pgpass|postgres://|dsn)') "published metadata contains no credential-like fields"
} finally {
    if (Test-Path -LiteralPath $testRoot) {
        Remove-Item -LiteralPath $testRoot -Recurse -Force -Confirm:$false
    }
}

Check (-not (Test-Path -LiteralPath $testRoot)) "throwaway test root removed"

if ($script:fail -gt 0) {
    Write-Output "RESULT: $($script:fail) failure(s), $($script:pass) passed"
    exit 1
}
Write-Output "RESULT: all $($script:pass) backup-publish tests passed"
exit 0
