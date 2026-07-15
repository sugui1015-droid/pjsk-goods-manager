# Invoke-MigrationFactsTests.ps1 — offline tests for _MigrationFacts.ps1, the
# shared lookup that keeps drill fixtures and restore verification from
# hardcoding a migration count or maximum version.
#
# Requires NO database, NO password, and NO PostgreSQL tools. Every synthetic
# case builds its own throwaway directory OUTSIDE the repository and removes it
# at the end. Real backup directories are never scanned or touched; the real
# repository migrations directory is only ever listed, never modified.
[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'
$scriptDir = $PSScriptRoot
$script:fail = 0
$script:pass = 0

function Check([bool]$Condition, [string]$Name) {
    if ($Condition) { Write-Output "PASS  $Name"; $script:pass++ } else { Write-Output "FAIL  $Name"; $script:fail++ }
}

# Dot-source the param-less common file so its functions are available without
# clobbering caller scope.
. (Join-Path $scriptDir '_MigrationFacts.ps1')

$testRoot = Join-Path ([System.IO.Path]::GetTempPath()) ('pjsk-migfacts-tests-' + [guid]::NewGuid().ToString('N').Substring(0, 10))
New-Item -ItemType Directory -Force -Path $testRoot | Out-Null

function New-MigrationDir {
    param([string]$Name, [string[]]$Files, [string[]]$NestedFiles = @())
    $dir = Join-Path $testRoot $Name
    New-Item -ItemType Directory -Force -Path $dir | Out-Null
    foreach ($file in $Files) {
        Set-Content -LiteralPath (Join-Path $dir $file) -Value 'select 1;' -Encoding ascii
    }
    if ($NestedFiles.Count -gt 0) {
        $nested = Join-Path $dir 'nested'
        New-Item -ItemType Directory -Force -Path $nested | Out-Null
        foreach ($file in $NestedFiles) {
            Set-Content -LiteralPath (Join-Path $nested $file) -Value 'select 1;' -Encoding ascii
        }
    }
    return $dir
}

try {
    # --- the real repository migrations directory resolves and is valid ---
    # Deliberately NOT asserting an exact count here: pinning "19" is the very
    # staleness this helper exists to prevent. Exact-count behavior is proven
    # against synthetic directories below.
    $repoDir = Resolve-RepositoryMigrationsDirectory -ScriptDirectory $scriptDir
    Check (Test-Path -LiteralPath $repoDir -PathType Container) "repository migrations directory resolves: $repoDir"

    $repoFacts = $null
    $repoError = $null
    try { $repoFacts = Get-MigrationFacts -MigrationsDirectory $repoDir } catch { $repoError = $_ }
    Check ($null -eq $repoError) "current repository migrations validate without error"
    if ($repoFacts) {
        Check ($repoFacts.Count -ge 19) "repository migration count is at least the 19 known at 2026-07-15 (actual: $($repoFacts.Count))"
        Check ($repoFacts.MaxVersion -match (Get-MigrationFileNamePattern)) "repository max version is a well-formed name: $($repoFacts.MaxVersion)"
        Check ($repoFacts.Names -contains '0005_import_history.sql' -and $repoFacts.Names -contains '0005_product_series.sql') "both historical 0005 migrations are listed"
        Check ($repoFacts.Names -contains '0019_admin_auth_audit_events.sql') "0019_admin_auth_audit_events.sql is listed"
        Check ($repoFacts.Count -eq $repoFacts.Names.Count) "count equals the number of listed names"
    } else {
        Check $false "repository facts were produced"
    }

    # --- exact counts and max version, on a controlled synthetic directory ---
    $baseFiles = @(
        '0001_core_tables.sql', '0002_import_tracking.sql', '0005_import_history.sql',
        '0005_product_series.sql', '0007_import_revert.sql', '0019_admin_auth_audit_events.sql'
    )
    $baseDir = New-MigrationDir -Name 'base' -Files $baseFiles
    $baseFacts = Get-MigrationFacts -MigrationsDirectory $baseDir
    Check ($baseFacts.Count -eq 6) "synthetic set of 6 migrations counts 6 (actual: $($baseFacts.Count))"
    Check ($baseFacts.MaxVersion -eq '0019_admin_auth_audit_events.sql') "synthetic max version is 0019_admin_auth_audit_events.sql"
    Check ($baseFacts.Names[0] -eq '0001_core_tables.sql') "names are sorted ascending by full filename"
    Check ($baseFacts.Names[2] -eq '0005_import_history.sql' -and $baseFacts.Names[3] -eq '0005_product_series.sql') "duplicate 0005 prefixes both listed, import_history first"

    # --- adding a legitimate new migration moves the expectation automatically ---
    $grownDir = New-MigrationDir -Name 'grown' -Files ($baseFiles + '0020_future_feature.sql')
    $grownFacts = Get-MigrationFacts -MigrationsDirectory $grownDir
    Check ($grownFacts.Count -eq ($baseFacts.Count + 1)) "adding 0020 raises the expected count to $($baseFacts.Count + 1) (actual: $($grownFacts.Count))"
    Check ($grownFacts.MaxVersion -eq '0020_future_feature.sql') "adding 0020 moves the expected max version to 0020_future_feature.sql"
    Check ($grownFacts.MaxVersion -ne $baseFacts.MaxVersion) "expected max version is not frozen at the previous value"

    # --- non-.sql files are not counted ---
    $mixedDir = New-MigrationDir -Name 'mixed' -Files @('0001_core_tables.sql', '0002_import_tracking.sql')
    Set-Content -LiteralPath (Join-Path $mixedDir 'readme.txt') -Value 'notes' -Encoding ascii
    Set-Content -LiteralPath (Join-Path $mixedDir 'notes.md') -Value '# notes' -Encoding ascii
    Set-Content -LiteralPath (Join-Path $mixedDir '0003_backup.sql.bak') -Value 'select 1;' -Encoding ascii
    Set-Content -LiteralPath (Join-Path $mixedDir '0004_wrong.sqlx') -Value 'select 1;' -Encoding ascii
    $mixedFacts = Get-MigrationFacts -MigrationsDirectory $mixedDir
    Check ($mixedFacts.Count -eq 2) "non-.sql files (.txt/.md/.sql.bak/.sqlx) are not counted (actual: $($mixedFacts.Count))"

    # --- .sql files in subdirectories are not counted ---
    $nestedDir = New-MigrationDir -Name 'nested-case' -Files @('0001_core_tables.sql') -NestedFiles @('0002_nested.sql', '0003_nested.sql')
    $nestedFacts = Get-MigrationFacts -MigrationsDirectory $nestedDir
    Check ($nestedFacts.Count -eq 1) "nested .sql files are not counted (actual: $($nestedFacts.Count))"
    Check ($nestedFacts.MaxVersion -eq '0001_core_tables.sql') "nested .sql files do not affect the max version"

    # --- a missing directory fails loudly, with no fallback ---
    $missingDir = Join-Path $testRoot 'no-such-directory'
    $missingError = $null
    try { Get-MigrationFacts -MigrationsDirectory $missingDir | Out-Null } catch { $missingError = $_ }
    Check ($null -ne $missingError) "a missing migrations directory throws instead of returning a number"
    Check ($missingError -and "$($missingError.Exception.Message)" -match 'not found') "the missing-directory error names the cause"

    # --- an empty directory fails loudly rather than reporting zero ---
    $emptyDir = New-MigrationDir -Name 'empty' -Files @()
    $emptyError = $null
    try { Get-MigrationFacts -MigrationsDirectory $emptyDir | Out-Null } catch { $emptyError = $_ }
    Check ($null -ne $emptyError) "an empty migrations directory throws instead of reporting 0"

    # --- a malformed migration filename fails loudly ---
    $badDir = New-MigrationDir -Name 'bad-name' -Files @('0001_core_tables.sql', 'adhoc_patch.sql')
    $badError = $null
    try { Get-MigrationFacts -MigrationsDirectory $badDir | Out-Null } catch { $badError = $_ }
    Check ($null -ne $badError) "a migration filename without a 4-digit prefix throws"

    $shortPrefixDir = New-MigrationDir -Name 'short-prefix' -Files @('0001_core_tables.sql', '020_too_short.sql')
    $shortPrefixError = $null
    try { Get-MigrationFacts -MigrationsDirectory $shortPrefixDir | Out-Null } catch { $shortPrefixError = $_ }
    Check ($null -ne $shortPrefixError) "a 3-digit prefix throws (byte order would stop matching numeric order)"

    # --- no silent fallback to a fixed number anywhere in the helper ---
    $commonContent = Get-Content -LiteralPath (Join-Path $scriptDir '_MigrationFacts.ps1') -Raw
    Check ($commonContent -notmatch '(?m)^\s*(return|\$\w+\s*=)\s*1[89]\s*$') "the helper contains no hardcoded 18/19 fallback"

    # --- the consuming scripts no longer hardcode a migration count/version ---
    $backupContent = Get-Content -LiteralPath (Join-Path $scriptDir 'Backup-Postgres.ps1') -Raw
    Check ($backupContent -notmatch 'schema_migrations\s*=\s*\d+') "Backup-Postgres.ps1 does not assign a literal schema_migrations count"
    $verifyContent = Get-Content -LiteralPath (Join-Path $scriptDir 'Test-PostgresBackup.ps1') -Raw
    Check ($verifyContent -notmatch "migrationMax\s*-eq\s*'0") "Test-PostgresBackup.ps1 does not compare migrationMax to a literal version"

    # --- the helper never touches a database, a backup, or the filesystem ---
    Check ($commonContent -notmatch '(?i)(psql|pg_dump|pg_restore|Invoke-Sqlcmd|Remove-Item|Set-Content|Out-File|Move-Item|New-Item)') "the helper runs no database tool and writes no file"
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
Write-Output "RESULT: all $($script:pass) migration-facts tests passed"
exit 0
