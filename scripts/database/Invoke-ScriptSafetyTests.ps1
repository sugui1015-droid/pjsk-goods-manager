# Invoke-ScriptSafetyTests.ps1 — offline safety tests for the database
# scripts. Requires NO database connection and NO PostgreSQL password:
# every case either fails validation before any external command runs, or
# uses -DryRun. Also statically checks the scripts for forbidden patterns.
[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'
$script:failures = 0
$scriptDir = $PSScriptRoot
$powershell = (Get-Command powershell.exe).Source

function Invoke-Case {
    param(
        [string]$Name,
        [string[]]$Arguments,
        [int]$ExpectExitCode,
        [string[]]$OutputMustNotContain = @(),
        [string[]]$OutputMustContain = @()
    )
    # Child scripts legitimately write errors to stderr; collect everything
    # without letting PS 5.1 NativeCommandError records abort this runner.
    $previousPreference = $ErrorActionPreference
    $ErrorActionPreference = 'Continue'
    $output = (& $powershell -NoProfile -NonInteractive -ExecutionPolicy Bypass -File @Arguments 2>&1 | ForEach-Object { "$_" }) -join "`n"
    $code = $LASTEXITCODE
    $ErrorActionPreference = $previousPreference
    $ok = $true
    if (($ExpectExitCode -eq 0 -and $code -ne 0) -or ($ExpectExitCode -ne 0 -and $code -eq 0)) {
        $ok = $false
        Write-Output ("FAIL  {0}: exit code {1}, expected {2}" -f $Name, $code, $ExpectExitCode)
    }
    foreach ($needle in $OutputMustNotContain) {
        if ($output -match [regex]::Escape($needle)) {
            $ok = $false
            Write-Output ("FAIL  {0}: output leaked '{1}'" -f $Name, $needle)
        }
    }
    foreach ($needle in $OutputMustContain) {
        if ($output -notmatch [regex]::Escape($needle)) {
            $ok = $false
            Write-Output ("FAIL  {0}: output missing '{1}'" -f $Name, $needle)
        }
    }
    if ($ok) { Write-Output ("PASS  {0}" -f $Name) } else { $script:failures++ }
}

$backup = Join-Path $scriptDir 'Backup-Postgres.ps1'
$restore = Join-Path $scriptDir 'Restore-PostgresTest.ps1'
$verify = Join-Path $scriptDir 'Test-PostgresBackup.ps1'
$remove = Join-Path $scriptDir 'Remove-PostgresRestoreTest.ps1'
$repoRoot = [System.IO.Path]::GetFullPath((Join-Path $scriptDir '..\..'))
$outsideDir = Join-Path ([System.IO.Path]::GetTempPath()) 'pjsk backup safety tests'
New-Item -ItemType Directory -Force -Path $outsideDir | Out-Null
$fakeDump = Join-Path $outsideDir 'fake.dump'
Set-Content -LiteralPath $fakeDump -Value 'not a real dump' -Encoding ascii
$fakeSql = Join-Path $outsideDir 'fake.sql'
Set-Content -LiteralPath $fakeSql -Value 'select 1' -Encoding ascii
$missingBin = Join-Path $outsideDir 'no-such-postgres-bin'

# --- backup script validation ---
Invoke-Case 'backup: missing mandatory parameters fails' @($backup) 1
Invoke-Case 'backup: invalid database name rejected' @($backup, '-DatabaseName', 'Bad;Name', '-BackupRoot', $outsideDir) 1
Invoke-Case 'backup: repository-internal BackupRoot rejected' @($backup, '-DatabaseName', 'pjsk_backup_source_test_x', '-BackupRoot', $repoRoot) 1
Invoke-Case 'backup: relative BackupRoot rejected' @($backup, '-DatabaseName', 'pjsk_backup_source_test_x', '-BackupRoot', 'relative\path') 1
Invoke-Case 'backup: missing tools rejected' @($backup, '-DatabaseName', 'pjsk_backup_source_test_x', '-BackupRoot', $outsideDir, '-PostgresBin', $missingBin) 1
Invoke-Case 'backup: dry-run succeeds without touching a database' @($backup, '-DatabaseName', 'pjsk_backup_source_test_x', '-BackupRoot', $outsideDir, '-DryRun') 0 @('PGPASSWORD') @('DRY RUN')
Invoke-Case 'backup: isolated-source gate rejects production database' @($backup, '-DatabaseName', 'pjsk', '-BackupRoot', $outsideDir, '-RequireIsolatedSource', '-DryRun') 1
$fixedDump = Join-Path $outsideDir 'pjsk_backup_source_test_fixed.dump'
Invoke-Case 'backup: fixed isolated path previews partial and no-password mode' @($backup, '-DatabaseName', 'pjsk_backup_source_test_x', '-BackupRoot', $outsideDir, '-OutputPath', $fixedDump, '-Username', 'postgres', '-RequireIsolatedSource', '-DryRun') 0 @('PGPASSWORD', '--clean', '--create') @($fixedDump, ($fixedDump + '.partial'), '-w', 'pjsk_backup_source_test_x')
Set-Content -LiteralPath $fixedDump -Value 'collision sentinel' -Encoding ascii
Invoke-Case 'backup: fixed output refuses overwrite during dry-run' @($backup, '-DatabaseName', 'pjsk_backup_source_test_x', '-BackupRoot', $outsideDir, '-OutputPath', $fixedDump, '-RequireIsolatedSource', '-DryRun') 1 @() @('Refusing to overwrite')
Remove-Item -LiteralPath $fixedDump -Force

# --- restore script validation ---
Invoke-Case 'restore: production name pjsk rejected' @($restore, '-BackupFile', $fakeDump, '-TargetDatabase', 'pjsk') 1
Invoke-Case 'restore: postgres rejected' @($restore, '-BackupFile', $fakeDump, '-TargetDatabase', 'postgres') 1
Invoke-Case 'restore: template1 rejected' @($restore, '-BackupFile', $fakeDump, '-TargetDatabase', 'template1') 1
Invoke-Case 'restore: missing test prefix rejected' @($restore, '-BackupFile', $fakeDump, '-TargetDatabase', 'pjsk_other_db') 1
Invoke-Case 'restore: uppercase target rejected' @($restore, '-BackupFile', $fakeDump, '-TargetDatabase', 'pjsk_restore_test_ABC') 1
Invoke-Case 'restore: missing dump file rejected' @($restore, '-BackupFile', (Join-Path $outsideDir 'missing.dump'), '-TargetDatabase', 'pjsk_restore_test_x') 1
Invoke-Case 'restore: non-dump extension rejected' @($restore, '-BackupFile', $fakeSql, '-TargetDatabase', 'pjsk_restore_test_x') 1
Invoke-Case 'restore: missing tools rejected' @($restore, '-BackupFile', $fakeDump, '-TargetDatabase', 'pjsk_restore_test_x', '-PostgresBin', $missingBin) 1
Invoke-Case 'restore: dry-run succeeds and forbids clean/create' @($restore, '-BackupFile', $fakeDump, '-TargetDatabase', 'pjsk_restore_test_x', '-DryRun') 0 @('PGPASSWORD') @('never used', '--clean')

# --- verification script validation ---
Invoke-Case 'verify: non-test restored database rejected' @($verify, '-RestoredDatabase', 'pjsk') 1
Invoke-Case 'verify: non-test source database rejected' @($verify, '-RestoredDatabase', 'pjsk_restore_test_x', '-SourceDatabase', 'pjsk') 1
Invoke-Case 'verify: restore database cannot be used as source' @($verify, '-RestoredDatabase', 'pjsk_restore_test_x', '-SourceDatabase', 'pjsk_restore_test_y') 1

# --- cleanup script validation ---
Invoke-Case 'remove: pjsk rejected' @($remove, '-TargetDatabase', 'pjsk') 1
Invoke-Case 'remove: postgres rejected' @($remove, '-TargetDatabase', 'postgres') 1
Invoke-Case 'remove: template0 rejected' @($remove, '-TargetDatabase', 'template0') 1
Invoke-Case 'remove: arbitrary name rejected' @($remove, '-TargetDatabase', 'some_other_db') 1
Invoke-Case 'remove: wildcard-looking name rejected' @($remove, '-TargetDatabase', 'pjsk_restore_test_%') 1
Invoke-Case 'remove: dry-run succeeds' @($remove, '-TargetDatabase', 'pjsk_restore_test_x', '-DryRun') 0 @('PGPASSWORD') @('DRY RUN')

# --- backup dry-run with spaces in path ---
$spacedDir = Join-Path $outsideDir 'with spaces'
New-Item -ItemType Directory -Force -Path $spacedDir | Out-Null
Invoke-Case 'backup: path with spaces handled' @($backup, '-DatabaseName', 'pjsk_backup_source_test_x', '-BackupRoot', $spacedDir, '-DryRun') 0 @() @('with spaces')

# --- static content checks ---
foreach ($file in @($backup, $restore, $verify, $remove)) {
    $content = Get-Content -LiteralPath $file -Raw
    $name = [System.IO.Path]::GetFileName($file)
    if ($content -match 'Invoke-Expression') {
        Write-Output "FAIL  static: $name uses Invoke-Expression"; $script:failures++
    } else { Write-Output "PASS  static: $name avoids Invoke-Expression" }
    if ($content -match 'cmd\s*/c') {
        Write-Output "FAIL  static: $name shells out via cmd /c"; $script:failures++
    } else { Write-Output "PASS  static: $name avoids cmd /c" }
    if ($content -match "'--clean'") {
        Write-Output "FAIL  static: $name passes --clean as an argument"; $script:failures++
    } else { Write-Output "PASS  static: $name never passes --clean" }
    if ($content -match "'--create'") {
        Write-Output "FAIL  static: $name passes --create as an argument"; $script:failures++
    } else { Write-Output "PASS  static: $name never passes --create" }
    if ($content -match '(?i)password\s*=' ) {
        Write-Output "FAIL  static: $name assigns a password"; $script:failures++
    } else { Write-Output "PASS  static: $name never assigns a password" }
}

Remove-Item -LiteralPath $outsideDir -Recurse -Force -Confirm:$false

# Also run the backup-retention safety tests (report + cleanup scripts).
Write-Output "--- retention tooling safety tests ---"
$retentionTests = Join-Path $scriptDir 'Invoke-RetentionSafetyTests.ps1'
$prevPref = $ErrorActionPreference
$ErrorActionPreference = 'Continue'
& $powershell -NoProfile -NonInteractive -ExecutionPolicy Bypass -File $retentionTests
$retentionExit = $LASTEXITCODE
$ErrorActionPreference = $prevPref
if ($retentionExit -ne 0) { $script:failures++ }

# Also run the migration-facts tests (drill fixture / verification expectations).
Write-Output "--- migration facts tests ---"
$migrationTests = Join-Path $scriptDir 'Invoke-MigrationFactsTests.ps1'
$prevPref = $ErrorActionPreference
$ErrorActionPreference = 'Continue'
& $powershell -NoProfile -NonInteractive -ExecutionPolicy Bypass -File $migrationTests
$migrationExit = $LASTEXITCODE
$ErrorActionPreference = $prevPref
if ($migrationExit -ne 0) { $script:failures++ }

# Also run the backup publish-path tests (real vs isolated-drill backup modes).
# These use mock pg_dump/pg_restore, never a real database, and cover the
# publish path that every -DryRun test above exits before reaching.
Write-Output "--- backup publish path tests ---"
$publishTests = Join-Path $scriptDir 'Invoke-BackupPublishTests.ps1'
$prevPref = $ErrorActionPreference
$ErrorActionPreference = 'Continue'
& $powershell -NoProfile -NonInteractive -ExecutionPolicy Bypass -File $publishTests
$publishExit = $LASTEXITCODE
$ErrorActionPreference = $prevPref
if ($publishExit -ne 0) { $script:failures++ }

if ($script:failures -gt 0) {
    Write-Output "RESULT: $script:failures failure(s)"
    exit 1
}
Write-Output "RESULT: all safety tests passed"
exit 0
