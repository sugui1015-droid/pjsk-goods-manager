# Invoke-BackupValidationTests.ps1 — offline tests for the validation closure:
# Test-PostgresBackup.ps1 publishing a .validation.json, and retention reading it
# back as 'verified'. Also covers retention's strict handling of a historical
# metadata isolatedTestBackup that is not a real boolean.
#
# NO real PostgreSQL is involved. A minimal mock psql.exe is compiled with the
# .NET Framework C# compiler that ships with Windows into a throwaway directory,
# so the real script's real publish path executes verbatim. Never connects to a
# database; never reads or writes any real backup directory.
[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'
$scriptDir = $PSScriptRoot
$verify = Join-Path $scriptDir 'Test-PostgresBackup.ps1'
$script:fail = 0
$script:pass = 0

function Check([bool]$Condition, [string]$Name) {
    if ($Condition) { Write-Output "PASS  $Name"; $script:pass++ } else { Write-Output "FAIL  $Name"; $script:fail++ }
}

. (Join-Path $scriptDir '_BackupMetadata.ps1')
. (Join-Path $scriptDir '_RetentionCommon.ps1')

# Throwaway root OUTSIDE the repo and outside any protected directory. It cannot
# live under %TEMP% because that sits inside the user profile, which
# Test-BackupRootGuard rightly refuses to scan — same reason and same convention
# as Invoke-RetentionSafetyTests.ps1 (D:, no "pjsk" in the name).
$testRoot = "D:\bkvalidation-tests-" + [guid]::NewGuid().ToString('N').Substring(0, 10)
New-Item -ItemType Directory -Force -Path $testRoot | Out-Null

try {
    # ---------------------------------------------------------------------
    # Mock psql (NOT real psql). Answers the fixed catalog queries that
    # Test-PostgresBackup.ps1 issues. PJSK_MOCK_BREAK forces a check to fail.
    # ---------------------------------------------------------------------
    $csc = Join-Path $env:WINDIR 'Microsoft.NET\Framework64\v4.0.30319\csc.exe'
    if (-not (Test-Path -LiteralPath $csc)) {
        Write-Output "FAIL  prerequisite: C# compiler not found at $csc"
        exit 1
    }
    $mockBin = Join-Path $testRoot 'mock-bin'
    New-Item -ItemType Directory -Force -Path $mockBin | Out-Null
    $psqlSource = Join-Path $testRoot 'mock_psql.cs'
    Set-Content -LiteralPath $psqlSource -Encoding ascii -Value @'
using System;
class MockPsql {
    static int Main(string[] args) {
        for (int i = 0; i < args.Length; i++)
            if (args[i] == "--version") { Console.WriteLine("psql (PostgreSQL) 18.0 (MOCK - NOT REAL)"); return 0; }
        string q = null;
        for (int i = 0; i < args.Length; i++)
            if (args[i] == "-c" && i + 1 < args.Length) { q = args[i + 1]; break; }
        if (q == null) return 1;
        bool broken = Environment.GetEnvironmentVariable("PJSK_MOCK_BREAK") == "1";
        if (q.Contains("max(version)")) { Console.WriteLine("0019_admin_auth_audit_events.sql"); return 0; }
        if (q.Contains("count(*) from schema_migrations")) { Console.WriteLine("19"); return 0; }
        if (q.Contains("information_schema.tables")) { Console.WriteLine("21"); return 0; }
        if (q.Contains("contype = 'p'")) { Console.WriteLine("21"); return 0; }
        if (q.Contains("contype = 'f'")) { Console.WriteLine("13"); return 0; }
        if (q.Contains("pg_indexes")) { Console.WriteLine("53"); return 0; }
        if (q.Contains("information_schema.sequences")) { Console.WriteLine("0"); return 0; }
        if (q.Contains("pg_extension")) { Console.WriteLine("1"); return 0; }
        if (q.Contains("gen_random_uuid")) { Console.WriteLine("1"); return 0; }
        if (q.Contains("to_regclass")) { Console.WriteLine(broken ? "0" : "1"); return 0; }
        if (q.Contains("count(*) from public.")) { Console.WriteLine("2"); return 0; }
        Console.WriteLine("0");
        return 0;
    }
}
'@
    & $csc '/nologo' '/target:exe' ("/out:" + (Join-Path $mockBin 'psql.exe')) $psqlSource | Out-Null
    Check ($LASTEXITCODE -eq 0 -and (Test-Path (Join-Path $mockBin 'psql.exe'))) "mock psql built (no real PostgreSQL involved)"

    $powershell = (Get-Command powershell.exe).Source
    function Invoke-Verify {
        param([string[]]$Arguments)
        $previous = $ErrorActionPreference
        $ErrorActionPreference = 'Continue'
        $output = (& $powershell -NoProfile -NonInteractive -ExecutionPolicy Bypass -File $verify @Arguments 2>&1 | ForEach-Object { "$_" }) -join "`n"
        $code = $LASTEXITCODE
        $ErrorActionPreference = $previous
        return [pscustomobject]@{ Output = $output; ExitCode = $code }
    }

    # Builds a dump + metadata pair the way Backup-Postgres.ps1 would.
    function New-BackupSetFixture {
        param(
            [string]$Dir, [string]$Base, [bool]$Isolated = $false, [string]$Content = 'MOCK DUMP CONTENT',
            [switch]$OmitIsolated, [switch]$UseOverride, $OverrideValue = $null
        )
        New-Item -ItemType Directory -Force -Path $Dir | Out-Null
        $dump = Join-Path $Dir ($Base + '.dump')
        Set-Content -LiteralPath $dump -Value $Content -Encoding ascii -NoNewline
        $sha = (Get-FileHash -LiteralPath $dump -Algorithm SHA256).Hash
        $size = (Get-Item -LiteralPath $dump).Length
        $meta = [ordered]@{
            schemaVersion = 1; createdAtUtc = ([datetime]::UtcNow).ToString('o')
            sourceDatabaseName = $(if ($Isolated) { 'pjsk_backup_source_test_x' } else { 'pjsk' })
            dumpFormat = 'custom'; dumpFileName = ($Base + '.dump'); dumpSizeBytes = $size; dumpSha256 = $sha
        }
        # OmitIsolated -> field absent; UseOverride -> write exactly the given
        # value (including $null, a string, or a number); otherwise a real boolean.
        if ($OmitIsolated) { }
        elseif ($UseOverride) { $meta.isolatedTestBackup = $OverrideValue }
        else { $meta.isolatedTestBackup = $Isolated }
        ($meta | ConvertTo-Json) | Out-File -LiteralPath (Join-Path $Dir ($Base + '.metadata.json')) -Encoding utf8
        return [pscustomobject]@{ Dump = $dump; Sha = $sha; Size = $size; Base = $Base; Dir = $Dir }
    }

    # =====================================================================
    # 1. Successful drill publishes a final validation.
    # =====================================================================
    $okDir = Join-Path $testRoot 'ok'
    $fx = New-BackupSetFixture -Dir $okDir -Base 'pjsk-ok' -Isolated $false
    $validationPath = Join-Path $okDir 'pjsk-ok.validation.json'
    $result = Invoke-Verify @('-RestoredDatabase', 'pjsk_restore_test_a', '-PostgresBin', $mockBin, '-BackupFile', $fx.Dump)

    Check ($result.ExitCode -eq 0) "passing drill exits 0"
    Check (Test-Path -LiteralPath $validationPath) "passing drill publishes a final .validation.json"
    Check (-not (Test-Path -LiteralPath ($validationPath + '.partial'))) "no validation .partial left behind"
    Check (Test-Path -LiteralPath $fx.Dump) "the dump is untouched"
    Check (Test-Path -LiteralPath (Join-Path $okDir 'pjsk-ok.metadata.json')) "the metadata is untouched"
    Check (-not (Test-Path -LiteralPath (Join-Path $okDir 'pjsk-ok.validation-failed.json'))) "no failure report on success"

    $v = $null
    if (Test-Path -LiteralPath $validationPath) { $v = Get-Content -LiteralPath $validationPath -Raw | ConvertFrom-Json }
    # Field names must match what _RetentionCommon.ps1 actually reads.
    Check ($v -and $v.overallResult -eq 'passed') "validation uses overallResult='passed' (the only value retention accepts)"
    Check ($v -and $v.dumpSha256 -eq $fx.Sha) "validation dumpSha256 equals the dump's real hash"
    Check ($v -and $v.backupFileName -eq 'pjsk-ok.dump') "validation binds the dump by file name"
    Check ($v -and $v.metadataFileName -eq 'pjsk-ok.metadata.json') "validation binds the metadata by file name"
    Check ($v -and $v.isolatedTestBackup -is [bool] -and $v.isolatedTestBackup -eq $false) "validation carries isolatedTestBackup as a real boolean"
    Check ($v -and $v.schemaVersion -is [int]) "schemaVersion is a number, not a string"
    Check ($v -and ($v.dumpSizeBytes -is [int] -or $v.dumpSizeBytes -is [long]) -and [long]$v.dumpSizeBytes -eq $fx.Size) "dumpSizeBytes is a number matching the dump"
    Check ($v -and $v.migrationCount -is [int] -and $v.migrationCount -eq 19) "migrationCount is a number"
    Check ($v -and $v.migrationMax -eq '0019_admin_auth_audit_events.sql') "migrationMax recorded"
    Check ($v -and $v.restoreDatabaseName -eq 'pjsk_restore_test_a') "restoreDatabaseName records only the throwaway test database name"
    $validatedOk = $false
    if ($v) { try { $null = [datetime]::Parse($v.validatedUtc); $validatedOk = ($v.validatedUtc -match 'Z|\+00:00') } catch {} }
    Check $validatedOk "validatedUtc is a parseable UTC timestamp"

    # No credentials or connection details may ever land in a validation.
    $vRaw = ''
    if (Test-Path -LiteralPath $validationPath) { $vRaw = Get-Content -LiteralPath $validationPath -Raw }
    Check ($vRaw -notmatch '(?i)(password|pgpass|postgres://|dsn|token|cookie|authorization|private key|BEGIN )') "validation contains no credential-like fields"
    Check ($vRaw -notmatch '(?i)(--host|--port|--username|127\.0\.0\.1|5432)') "validation contains no host, port, username, or command line"

    # =====================================================================
    # 2. Retention reads the published validation as verified.
    # =====================================================================
    $report = Get-PostgresBackupRetentionReport -BackupRoot $okDir -VerifyHash
    $set = @($report | Where-Object { $_.SetId -eq 'pjsk-ok' })[0]
    Check ($null -ne $set) "retention finds the backup set"
    Check ($set -and $set.Status -eq 'verified') "retention reads the new validation as verified (closing the loop)"
    Check ($set -and $set.IsolatedTest -eq $false) "retention classifies it as a real (non-drill) backup"

    # =====================================================================
    # 3. Refusing to overwrite an existing validation.
    # =====================================================================
    $result = Invoke-Verify @('-RestoredDatabase', 'pjsk_restore_test_b', '-PostgresBin', $mockBin, '-BackupFile', $fx.Dump)
    Check ($result.ExitCode -ne 0) "a second drill refuses to overwrite an existing validation"
    $v2 = Get-Content -LiteralPath $validationPath -Raw | ConvertFrom-Json
    Check ($v2.restoreDatabaseName -eq 'pjsk_restore_test_a') "the original validation is left untouched"

    # =====================================================================
    # 4. A failed drill must NOT publish a validation.
    # =====================================================================
    $failDir = Join-Path $testRoot 'failing'
    $fxFail = New-BackupSetFixture -Dir $failDir -Base 'pjsk-fail' -Isolated $false
    $failValidation = Join-Path $failDir 'pjsk-fail.validation.json'
    $env:PJSK_MOCK_BREAK = '1'
    $result = Invoke-Verify @('-RestoredDatabase', 'pjsk_restore_test_c', '-PostgresBin', $mockBin, '-BackupFile', $fxFail.Dump)
    Remove-Item Env:\PJSK_MOCK_BREAK -ErrorAction SilentlyContinue

    Check ($result.ExitCode -ne 0) "a failing drill exits non-zero"
    Check (-not (Test-Path -LiteralPath $failValidation)) "a failing drill publishes NO .validation.json"
    Check (-not (Test-Path -LiteralPath ($failValidation + '.partial'))) "a failing drill leaves no validation .partial"
    Check (Test-Path -LiteralPath (Join-Path $failDir 'pjsk-fail.validation-failed.json')) "a failing drill leaves a failure report under a different name"
    Check (Test-Path -LiteralPath $fxFail.Dump) "a failing drill does not delete the dump"
    Check (Test-Path -LiteralPath (Join-Path $failDir 'pjsk-fail.metadata.json')) "a failing drill does not delete the metadata"

    # The failure report must be invisible to retention, never read as success.
    $failReport = Get-PostgresBackupRetentionReport -BackupRoot $failDir -VerifyHash
    $failSet = @($failReport | Where-Object { $_.SetId -eq 'pjsk-fail' })[0]
    Check ($failSet -and $failSet.Status -ne 'verified') "retention never treats a failure report as verified"
    Check ($failSet -and $failSet.Decision -eq 'Protected') "a failed-drill backup is Protected, never auto-deleted"

    # =====================================================================
    # 5. Binding failures: metadata missing / invalid / dump changed.
    # =====================================================================
    $noMetaDir = Join-Path $testRoot 'no-meta'
    New-Item -ItemType Directory -Force -Path $noMetaDir | Out-Null
    $lonelyDump = Join-Path $noMetaDir 'pjsk-lonely.dump'
    Set-Content -LiteralPath $lonelyDump -Value 'X' -Encoding ascii
    $result = Invoke-Verify @('-RestoredDatabase', 'pjsk_restore_test_d', '-PostgresBin', $mockBin, '-BackupFile', $lonelyDump)
    Check ($result.ExitCode -ne 0) "missing metadata fails the drill"
    Check (-not (Test-Path -LiteralPath (Join-Path $noMetaDir 'pjsk-lonely.validation.json'))) "missing metadata publishes no validation"

    $badMetaDir = Join-Path $testRoot 'bad-meta'
    $fxBad = New-BackupSetFixture -Dir $badMetaDir -Base 'pjsk-bad' -Isolated $false
    Set-Content -LiteralPath (Join-Path $badMetaDir 'pjsk-bad.metadata.json') -Value '{ not json' -Encoding utf8
    $result = Invoke-Verify @('-RestoredDatabase', 'pjsk_restore_test_e', '-PostgresBin', $mockBin, '-BackupFile', $fxBad.Dump)
    Check ($result.ExitCode -ne 0) "unparseable metadata fails the drill"
    Check (-not (Test-Path -LiteralPath (Join-Path $badMetaDir 'pjsk-bad.validation.json'))) "unparseable metadata publishes no validation"

    $strMetaDir = Join-Path $testRoot 'string-flag'
    $fxStr = New-BackupSetFixture -Dir $strMetaDir -Base 'pjsk-str' -UseOverride -OverrideValue 'false'
    $result = Invoke-Verify @('-RestoredDatabase', 'pjsk_restore_test_f', '-PostgresBin', $mockBin, '-BackupFile', $fxStr.Dump)
    Check ($result.ExitCode -ne 0) "metadata with a stringly-typed isolatedTestBackup fails the drill"
    Check (-not (Test-Path -LiteralPath (Join-Path $strMetaDir 'pjsk-str.validation.json'))) "untrustworthy metadata publishes no validation"

    $tamperDir = Join-Path $testRoot 'tampered'
    $fxTamper = New-BackupSetFixture -Dir $tamperDir -Base 'pjsk-tamper' -Isolated $false
    Set-Content -LiteralPath $fxTamper.Dump -Value 'TAMPERED CONTENT' -Encoding ascii -NoNewline
    $result = Invoke-Verify @('-RestoredDatabase', 'pjsk_restore_test_g', '-PostgresBin', $mockBin, '-BackupFile', $fxTamper.Dump)
    Check ($result.ExitCode -ne 0) "a dump changed since backup fails the drill (hash mismatch)"
    Check (-not (Test-Path -LiteralPath (Join-Path $tamperDir 'pjsk-tamper.validation.json'))) "a tampered dump publishes no validation"

    # A failing drill must not destroy a pre-existing valid validation.
    $keepDir = Join-Path $testRoot 'keep-existing'
    $fxKeep = New-BackupSetFixture -Dir $keepDir -Base 'pjsk-keep' -Isolated $false
    $keepValidation = Join-Path $keepDir 'pjsk-keep.validation.json'
    Set-Content -LiteralPath $keepValidation -Value '{"overallResult":"passed"}' -Encoding utf8
    $keepHash = (Get-FileHash -LiteralPath $keepValidation -Algorithm SHA256).Hash
    $env:PJSK_MOCK_BREAK = '1'
    $result = Invoke-Verify @('-RestoredDatabase', 'pjsk_restore_test_h', '-PostgresBin', $mockBin, '-BackupFile', $fxKeep.Dump)
    Remove-Item Env:\PJSK_MOCK_BREAK -ErrorAction SilentlyContinue
    Check ($result.ExitCode -ne 0) "a drill against an already-validated backup is refused"
    Check ((Get-FileHash -LiteralPath $keepValidation -Algorithm SHA256).Hash -eq $keepHash) "an existing valid validation survives a failing drill"

    # =====================================================================
    # 6. Drill-mode backup: isolated flag flows through to the validation.
    # =====================================================================
    $isoDir = Join-Path $testRoot 'isolated'
    $fxIso = New-BackupSetFixture -Dir $isoDir -Base 'pjsk-iso' -Isolated $true
    $result = Invoke-Verify @('-RestoredDatabase', 'pjsk_restore_test_i', '-PostgresBin', $mockBin, '-BackupFile', $fxIso.Dump)
    Check ($result.ExitCode -eq 0) "isolated drill backup validates successfully"
    $vIso = Get-Content -LiteralPath (Join-Path $isoDir 'pjsk-iso.validation.json') -Raw | ConvertFrom-Json
    Check ($vIso.isolatedTestBackup -eq $true) "validation records isolatedTestBackup = true for a drill backup"
    $isoReport = Get-PostgresBackupRetentionReport -BackupRoot $isoDir -VerifyHash
    $isoSet = @($isoReport | Where-Object { $_.SetId -eq 'pjsk-iso' })[0]
    Check ($isoSet -and $isoSet.Status -eq 'verified') "retention verifies the drill backup"
    Check ($isoSet -and $isoSet.Decision -eq 'Protected' -and $isoSet.DecisionReason -match 'isolated') "a verified drill backup stays Protected as drill evidence"

    # =====================================================================
    # 7. Without -BackupFile the script behaves exactly as before.
    # =====================================================================
    $plainDir = Join-Path $testRoot 'plain'
    New-Item -ItemType Directory -Force -Path $plainDir | Out-Null
    $result = Invoke-Verify @('-RestoredDatabase', 'pjsk_restore_test_j', '-PostgresBin', $mockBin)
    Check ($result.ExitCode -eq 0) "without -BackupFile the drill still runs and passes"
    Check ($result.Output -match 'RESULT: PASSED') "without -BackupFile the original PASS output is unchanged"
    Check (@(Get-ChildItem -Path $plainDir -File -ErrorAction SilentlyContinue).Count -eq 0) "without -BackupFile nothing is written"

    # =====================================================================
    # 8. Retention strict boolean compatibility for historical metadata.
    # =====================================================================
    # Publishes the resulting set into $script:lastFlagSet rather than returning
    # it: an assignment would capture this function's whole output stream, which
    # would swallow the Check lines along with the value.
    function Test-IsolatedFlagCase {
        param([string]$Name, [switch]$Omit, $Override, [string]$ExpectStatus, $ExpectIsolated)
        $dir = Join-Path $testRoot ('flag-' + $Name)
        if ($Omit) {
            $fx = New-BackupSetFixture -Dir $dir -Base ('pjsk-' + $Name) -OmitIsolated
        } else {
            $fx = New-BackupSetFixture -Dir $dir -Base ('pjsk-' + $Name) -UseOverride -OverrideValue $Override
        }
        # Give it a passing validation so it would otherwise reach 'verified' —
        # only the isolatedTestBackup type can hold it back.
        $rec = [ordered]@{ overallResult = 'passed'; dumpSha256 = $fx.Sha }
        ($rec | ConvertTo-Json) | Out-File -LiteralPath (Join-Path $dir ('pjsk-' + $Name + '.validation.json')) -Encoding utf8
        $rep = Get-PostgresBackupRetentionReport -BackupRoot $dir -VerifyHash
        $s = @($rep | Where-Object { $_.SetId -eq ('pjsk-' + $Name) })[0]
        $script:lastFlagSet = $s
        Check ($null -ne $s -and $s.Status -eq $ExpectStatus) "isolatedTestBackup $Name -> status '$ExpectStatus' (actual: $(if ($s) { $s.Status } else { 'no set' }))"
        Check ($null -ne $s -and $s.IsolatedTest -eq $ExpectIsolated) "isolatedTestBackup $Name -> IsolatedTest is '$ExpectIsolated', not silently coerced"
        if ($ExpectStatus -ne 'verified') {
            Check ($null -ne $s -and $s.Decision -ne 'Candidate') "isolatedTestBackup $Name -> never becomes a deletion candidate"
        }
    }

    Test-IsolatedFlagCase -Name 'booltrue'  -Override $true   -ExpectStatus 'verified' -ExpectIsolated $true
    Test-IsolatedFlagCase -Name 'boolfalse' -Override $false  -ExpectStatus 'verified' -ExpectIsolated $false
    Test-IsolatedFlagCase -Name 'strtrue'   -Override 'true'  -ExpectStatus 'unknown'  -ExpectIsolated $null
    Test-IsolatedFlagCase -Name 'strfalse'  -Override 'false' -ExpectStatus 'unknown'  -ExpectIsolated $null
    Test-IsolatedFlagCase -Name 'int1'      -Override 1       -ExpectStatus 'unknown'  -ExpectIsolated $null
    Test-IsolatedFlagCase -Name 'int0'      -Override 0       -ExpectStatus 'unknown'  -ExpectIsolated $null
    Test-IsolatedFlagCase -Name 'nullflag'  -Override $null   -ExpectStatus 'unknown'  -ExpectIsolated $null
    Test-IsolatedFlagCase -Name 'missing'   -Omit             -ExpectStatus 'unknown'  -ExpectIsolated $null

    $missingSet = $script:lastFlagSet
    Check ($null -ne $missingSet -and $missingSet.Decision -eq 'Error') "a missing isolatedTestBackup is flagged Error for human review"
    Check ($null -ne $missingSet -and "$($missingSet.Error)" -match 'isolatedTestBackup') "the error names isolatedTestBackup as the cause"

    # A string "false" must never be silently read as drill evidence, and must
    # never be silently read as a deletable real backup either.
    $strFalseDir = Join-Path $testRoot 'flag-strfalse'
    $strRep = Get-PostgresBackupRetentionReport -BackupRoot $strFalseDir -VerifyHash
    $strSet = @($strRep | Where-Object { $_.SetId -eq 'pjsk-strfalse' })[0]
    Check ($strSet -and $strSet.IsolatedTest -ne $true) 'string "false" is not silently coerced to drill evidence'
    Check ($strSet -and $strSet.Decision -ne 'Candidate') 'string "false" never becomes a deletion candidate'
} finally {
    Remove-Item Env:\PJSK_MOCK_BREAK -ErrorAction SilentlyContinue
    if (Test-Path -LiteralPath $testRoot) {
        Remove-Item -LiteralPath $testRoot -Recurse -Force -Confirm:$false
    }
}

Check (-not (Test-Path -LiteralPath $testRoot)) "throwaway test root removed"

if ($script:fail -gt 0) {
    Write-Output "RESULT: $($script:fail) failure(s), $($script:pass) passed"
    exit 1
}
Write-Output "RESULT: all $($script:pass) backup-validation tests passed"
exit 0
