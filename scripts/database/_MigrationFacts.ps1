# _MigrationFacts.ps1 — shared, read-only lookup of the repository's migration
# set, so drill fixtures and verification never hardcode a migration count or a
# maximum version that goes stale the moment a new migration lands.
#
# This file defines ONLY functions (no param block, no top-level actions), so it
# can be dot-sourced from any script without clobbering caller variables.
# Read-only: nothing here connects to a database, reads a backup, or writes any
# file. It only lists filenames under a migrations directory.

# Mirrors the naming rule enforced by the Go tests in
# backend/internal/database/migrations_test.go: a four-digit zero-padded prefix
# keeps byte-order sorting identical to numeric order, which is what both the
# migration runner and max(version) rely on.
function Get-MigrationFileNamePattern {
    return '^\d{4}_[a-z0-9_]+\.sql$'
}

function Resolve-RepositoryMigrationsDirectory {
    [CmdletBinding()]
    param([string]$ScriptDirectory)

    if ([string]::IsNullOrWhiteSpace($ScriptDirectory)) {
        throw "ScriptDirectory is required to resolve the migrations directory."
    }
    return [System.IO.Path]::GetFullPath((Join-Path $ScriptDirectory '..\..\backend\migrations'))
}

# Returns the repository's migration facts as an ordered hashtable:
#   Directory  — the resolved absolute directory
#   Names      — migration filenames, sorted by full filename (the runner's key)
#   Count      — number of migration files (expected schema_migrations row count
#                for a database migrated to the current repository state)
#   MaxVersion — greatest filename, i.e. the expected max(version)
#
# Throws on any condition that would otherwise tempt a silent fallback: missing
# directory, no .sql files, or a filename violating the naming rule. Callers
# must let it throw — never substitute a fixed number.
function Get-MigrationFacts {
    [CmdletBinding()]
    param([Parameter(Mandatory)][string]$MigrationsDirectory)

    if ([string]::IsNullOrWhiteSpace($MigrationsDirectory)) {
        throw "MigrationsDirectory is required."
    }
    $full = [System.IO.Path]::GetFullPath($MigrationsDirectory)
    if (-not (Test-Path -LiteralPath $full -PathType Container)) {
        throw "Migrations directory not found: $full"
    }

    # -File excludes directories; no -Recurse, so nested .sql files are ignored
    # exactly like the Go runner's non-recursive fs.ReadDir.
    $files = @(Get-ChildItem -LiteralPath $full -File -Filter '*.sql' |
        Where-Object { $_.Name.EndsWith('.sql', [System.StringComparison]::Ordinal) })

    if ($files.Count -eq 0) {
        throw "No .sql migration files found in: $full"
    }

    $pattern = Get-MigrationFileNamePattern
    $names = @()
    foreach ($file in $files) {
        if ($file.Name -notmatch $pattern) {
            throw "Migration filename does not match ${pattern}: $($file.Name)"
        }
        $names += $file.Name
    }

    # Ordinal sort so PowerShell's culture-aware default can never reorder these
    # relative to the Go runner's byte-order sort.
    $sorted = [string[]]$names
    [Array]::Sort($sorted, [System.StringComparer]::Ordinal)

    return [ordered]@{
        Directory  = $full
        Names      = $sorted
        Count      = $sorted.Count
        MaxVersion = $sorted[$sorted.Count - 1]
    }
}
