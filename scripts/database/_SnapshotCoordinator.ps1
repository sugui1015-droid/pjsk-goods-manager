# Pure validation and serialization helpers for Export-PostgresSnapshot.ps1.
# This file performs no database access and does not write files by itself.

Set-StrictMode -Version 2.0

$script:RequiredSnapshotPostgresVersion = '18.4'
$script:RequiredSnapshotServerVersionNum = '180004'

function Assert-SnapshotIdentifier([string]$Value, [string]$Name) {
    if ([string]::IsNullOrWhiteSpace($Value) -or $Value -notmatch '^[A-Za-z_][A-Za-z0-9_]{0,62}$') {
        throw "$Name must be a PostgreSQL identifier (letters, digits, underscore; maximum 63 characters)."
    }
}

function Assert-SnapshotHost([string]$Value) {
    if ([string]::IsNullOrWhiteSpace($Value) -or $Value -notmatch '^[A-Za-z0-9.:-]+$') {
        throw 'SourceHost contains unsupported characters.'
    }
}

function Assert-SnapshotMode {
    param(
        [Parameter(Mandatory)][ValidateSet('rehearsal', 'cutover')][string]$Mode,
        [switch]$WriteFreezeConfirmed,
        [string]$MaintenanceWindowId
    )
    if ($Mode -eq 'cutover') {
        if (-not $WriteFreezeConfirmed) {
            throw 'Cutover mode requires explicit -WriteFreezeConfirmed.'
        }
        if ([string]::IsNullOrWhiteSpace($MaintenanceWindowId) -or
            $MaintenanceWindowId -notmatch '^[a-z0-9][a-z0-9_-]{7,63}$') {
            throw 'Cutover mode requires a unique -MaintenanceWindowId (8-64 lowercase safe characters).'
        }
    } elseif ($WriteFreezeConfirmed -or $MaintenanceWindowId) {
        throw 'Write-freeze confirmation and maintenance-window ID are only valid in cutover mode.'
    }
}

function Assert-ArtifactBaseName([string]$Mode, [string]$BaseName) {
    if ($Mode -eq 'rehearsal') {
        if ($BaseName -notmatch '^pjsk-rehearsal-[0-9]{8}T[0-9]{9}Z$') {
            throw 'Rehearsal artifact name must use pjsk-rehearsal-<UTC>.'
        }
        if ($BaseName -match '(?i)final|cutover') {
            throw 'Rehearsal artifact names must never contain final or cutover.'
        }
    } else {
        if ($BaseName -notmatch '^pjsk-cutover-final-[a-z0-9][a-z0-9_-]{7,63}-[0-9]{8}T[0-9]{9}Z$') {
            throw 'Cutover artifact name must bind the maintenance-window ID and UTC timestamp.'
        }
    }
}

function Resolve-SnapshotOutputRoot {
    param(
        [Parameter(Mandatory)][string]$Path,
        [Parameter(Mandatory)][string]$RepositoryRoot,
        [Parameter(Mandatory)][ValidateSet('rehearsal', 'cutover')][string]$Mode
    )
    if ([string]::IsNullOrWhiteSpace($Path)) {
        throw 'OutputRoot must be an absolute path outside the repository.'
    }
    $full = [System.IO.Path]::GetFullPath($Path).TrimEnd('\', '/')
    if ($Mode -eq 'rehearsal') {
        $root = [System.IO.Path]::GetPathRoot($full)
        $remainder = $full.Substring($root.Length)
        $components = @($remainder -split '[\\/]' | Where-Object { $_ })
        if (@($components | Where-Object { $_ -match '(?i)(?:final|cutover)' }).Count -gt 0) {
            throw 'Rehearsal output path contains forbidden formal-release semantics (final or cutover).'
        }
    }
    if (-not [System.IO.Path]::IsPathRooted($Path)) {
        throw 'OutputRoot must be an absolute path outside the repository.'
    }
    Assert-OutsideRepository $full $RepositoryRoot
    return $full
}

function Assert-OutsideRepository([string]$Path, [string]$RepositoryRoot) {
    if (-not [System.IO.Path]::IsPathRooted($Path)) {
        throw 'OutputRoot must be an absolute path outside the repository.'
    }
    $full = [System.IO.Path]::GetFullPath($Path).TrimEnd('\')
    $repo = [System.IO.Path]::GetFullPath($RepositoryRoot).TrimEnd('\')
    if ($full -eq $repo -or ($full + '\').StartsWith($repo + '\', [System.StringComparison]::OrdinalIgnoreCase)) {
        throw 'OutputRoot must be outside the Git repository.'
    }
}

function Assert-NewOutputDirectory([string]$Path) {
    if (Test-Path -LiteralPath $Path) {
        throw 'The timestamped output directory already exists; refusing to overwrite it.'
    }
}

function Assert-AtomicPublicationPaths {
    param(
        [Parameter(Mandatory)][string]$StagingDirectory,
        [Parameter(Mandatory)][string]$FinalDirectory
    )
    $staging = [System.IO.Path]::GetFullPath($StagingDirectory).TrimEnd('\', '/')
    $final = [System.IO.Path]::GetFullPath($FinalDirectory).TrimEnd('\', '/')
    $stagingParent = [System.IO.DirectoryInfo]::new($staging).Parent.FullName.TrimEnd('\', '/')
    $finalParent = [System.IO.DirectoryInfo]::new($final).Parent.FullName.TrimEnd('\', '/')
    if ($stagingParent -cne $finalParent -or
        [System.IO.Path]::GetPathRoot($staging) -cne [System.IO.Path]::GetPathRoot($final)) {
        throw 'Staging and final directories must share the same parent and volume for atomic publication.'
    }
    if ([System.IO.Path]::GetFileName($staging) -notmatch '^\.pjsk-snapshot-staging-[0-9a-f]{32}$') {
        throw 'Staging directory must use the explicit hidden staging naming convention.'
    }
}

function Get-PostgresVersionInfo([string]$Text, [string]$ToolName) {
    $match = [regex]::Match("$Text", '(?i)PostgreSQL\)?\s+([0-9]+(?:\.[0-9]+)+)')
    if (-not $match.Success) {
        throw "Could not parse $ToolName version; required version is $script:RequiredSnapshotPostgresVersion."
    }
    $version = $match.Groups[1].Value
    $major = [int]($version.Split('.')[0])
    if ($version -cne $script:RequiredSnapshotPostgresVersion) {
        throw "$ToolName version is $version; required version is $script:RequiredSnapshotPostgresVersion."
    }
    [pscustomobject]@{ Version = $version; Major = $major }
}

function Assert-PostgresServerVersion {
    param(
        [Parameter(Mandatory)][string]$Version,
        [Parameter(Mandatory)][string]$VersionNumber
    )
    if ($VersionNumber -notmatch '^[0-9]{6}$') {
        throw "PostgreSQL server_version_num '$VersionNumber' is not parseable; required version is $script:RequiredSnapshotPostgresVersion ($script:RequiredSnapshotServerVersionNum)."
    }
    if ($Version -cne $script:RequiredSnapshotPostgresVersion -or
        $VersionNumber -cne $script:RequiredSnapshotServerVersionNum) {
        throw "PostgreSQL server version is $Version (server_version_num $VersionNumber); required version is $script:RequiredSnapshotPostgresVersion ($script:RequiredSnapshotServerVersionNum)."
    }
}

function ConvertTo-PgIdentifier([string]$Value) {
    if ($Value -notmatch '^[A-Za-z_][A-Za-z0-9_]{0,62}$') {
        throw 'Database catalog returned an unsafe identifier.'
    }
    return '"' + $Value.Replace('"', '""') + '"'
}

function Test-MigrationVersionSet {
    param([string[]]$Versions, [ref]$Reason)
    $Reason.Value = ''
    if ($null -eq $Versions -or $Versions.Count -eq 0) {
        $Reason.Value = 'schema_migrations is empty or missing.'
        return $false
    }
    foreach ($version in $Versions) {
        if ([string]::IsNullOrWhiteSpace($version) -or $version -notmatch '^[0-9]{4}_[a-z0-9_]+\.sql$') {
            $Reason.Value = 'schema_migrations contains an empty or unknown version format.'
            return $false
        }
    }
    if (@($Versions | Group-Object | Where-Object Count -gt 1).Count -gt 0) {
        $Reason.Value = 'schema_migrations contains duplicate versions.'
        return $false
    }
    $sorted = @($Versions | Sort-Object -CaseSensitive)
    if (($sorted -join "`n") -cne ($Versions -join "`n")) {
        $Reason.Value = 'schema_migrations is not in deterministic ordinal order.'
        return $false
    }
    return $true
}

function Test-MigrationSetExactMatch([string[]]$Expected, [string[]]$Actual) {
    if ($null -eq $Expected -or $null -eq $Actual) { return $false }
    return (($Expected -join "`n") -ceq ($Actual -join "`n"))
}

function Test-ExpectedSha256([string]$Path, [string]$Expected) {
    if (-not (Test-Path -LiteralPath $Path -PathType Leaf) -or $Expected -notmatch '^[0-9A-Fa-f]{64}$') { return $false }
    return ((Get-FileHash -LiteralPath $Path -Algorithm SHA256).Hash -ieq $Expected)
}

function Assert-StagingArtifactSet {
    param(
        [Parameter(Mandatory)][string]$StagingDirectory,
        [Parameter(Mandatory)][string]$ArtifactBaseName
    )
    $dumpPath = Join-Path $StagingDirectory ($ArtifactBaseName + '.dump')
    $expectedNames = @(
        ($ArtifactBaseName + '.dump')
        ($ArtifactBaseName + '.dump.sha256')
        'source-baseline.json'
        'source-migrations.txt'
        'export-metadata.json'
        'validation.json'
    )
    $entries = @(Get-ChildItem -LiteralPath $StagingDirectory -Force)
    if (@($entries | Where-Object { -not $_.PSIsContainer }).Count -ne 6 -or
        @($entries | Where-Object { $_.PSIsContainer }).Count -ne 0) {
        throw 'Staging directory must contain exactly six files and no subdirectories.'
    }
    $actualNames = @($entries | Where-Object { -not $_.PSIsContainer } | ForEach-Object Name | Sort-Object -CaseSensitive)
    if (($actualNames -join "`n") -cne (($expectedNames | Sort-Object -CaseSensitive) -join "`n")) {
        throw 'Staging directory does not contain exactly the six expected artifacts.'
    }
    if (@(Get-ChildItem -LiteralPath $StagingDirectory -File -Filter '*.partial').Count -ne 0) {
        throw 'A partial file remained before atomic publication.'
    }
    if ((Get-Item -LiteralPath $dumpPath).Length -le 0) {
        throw 'Finalized staging dump is empty.'
    }
    $shaModel = (Get-Content -LiteralPath ($dumpPath + '.sha256') -Raw).Trim()
    if ($shaModel -notmatch '^([0-9A-F]{64})  ([^\\/]+\.dump)$' -or
        $matches[2] -cne [IO.Path]::GetFileName($dumpPath) -or
        -not (Test-ExpectedSha256 $dumpPath $matches[1])) {
        throw 'Staging SHA-256 file is not bound to the finalized dump.'
    }
    foreach ($jsonName in @('source-baseline.json', 'export-metadata.json', 'validation.json')) {
        try { $null = Get-Content -LiteralPath (Join-Path $StagingDirectory $jsonName) -Raw | ConvertFrom-Json }
        catch { throw "Staging JSON artifact is invalid: $jsonName" }
    }
    $validation = Get-Content -LiteralPath (Join-Path $StagingDirectory 'validation.json') -Raw | ConvertFrom-Json
    if ($validation.verdict -cne 'PASS') { throw 'Staging validation verdict is not PASS.' }
    $migrations = @(Get-Content -LiteralPath (Join-Path $StagingDirectory 'source-migrations.txt') | ForEach-Object { $_.Trim() } | Where-Object { $_ })
    $migrationReason = ''
    if (-not (Test-MigrationVersionSet $migrations ([ref]$migrationReason))) {
        throw "Staging migration artifact failed validation: $migrationReason"
    }
}

function Test-NoFloatingPointJsonValue {
    param($Value)
    if ($null -eq $Value) { return $true }
    if ($Value -is [double] -or $Value -is [single] -or $Value -is [decimal]) { return $false }
    if ($Value -is [System.Collections.IDictionary]) {
        foreach ($key in $Value.Keys) {
            if (-not (Test-NoFloatingPointJsonValue $Value[$key])) { return $false }
        }
        return $true
    }
    if ($Value -is [System.Collections.IEnumerable] -and -not ($Value -is [string])) {
        foreach ($item in $Value) {
            if (-not (Test-NoFloatingPointJsonValue $item)) { return $false }
        }
    }
    return $true
}

function Test-SnapshotSensitiveText([string]$Text, [ref]$Reason) {
    $Reason.Value = ''
    $patterns = [ordered]@{
        private_key = '-----BEGIN (?:RSA |EC |OPENSSH )?PRIVATE KEY-----'
        database_url = '(?i)DATABASE_URL\s*='
        database_password = '(?i)DATABASE_PASSWORD\s*='
        pgpassword = '(?i)PGPASSWORD\s*='
        postgres_dsn = '(?i)postgres(?:ql)?://[^\s]+'
        recovery_key = '(?i)RECOVERY_EMAIL_(?:ENCRYPTION|HMAC)_KEY\s*='
        query_hmac = '(?i)QUERY_CODE_RECOVERY_HMAC_KEY\s*='
        smtp_password = '(?i)SMTP_PASSWORD\s*='
        bearer = '(?i)Authorization\s*:\s*Bearer\s+'
    }
    foreach ($name in $patterns.Keys) {
        if ($Text -match $patterns[$name]) {
            $Reason.Value = "sensitive marker category: $name"
            return $false
        }
    }
    return $true
}

function Get-CanonicalJson([System.Collections.IDictionary]$Value) {
    if (-not (Test-NoFloatingPointJsonValue $Value)) {
        throw 'JSON model contains a floating-point or decimal value; exact numerics must be strings.'
    }
    return ($Value | ConvertTo-Json -Depth 20)
}

function Test-PassfileAcl([string]$Path, [ref]$Reason) {
    $Reason.Value = ''
    if (-not (Test-Path -LiteralPath $Path -PathType Leaf)) {
        $Reason.Value = 'passfile does not exist.'
        return $false
    }
    try {
        $acl = Get-Acl -LiteralPath $Path
        $broad = @($acl.Access | Where-Object {
            $_.AccessControlType -eq 'Allow' -and
            $_.IdentityReference.Value -match '(?i)(Everyone|Authenticated Users|BUILTIN\\Users|Users)$' -and
            ($_.FileSystemRights.ToString() -match '(?i)Read|Write|FullControl|Modify')
        })
        if ($broad.Count -gt 0) {
            $Reason.Value = 'passfile grants broad read or write access.'
            return $false
        }
    } catch {
        $Reason.Value = 'passfile ACL could not be verified.'
        return $false
    }
    return $true
}
