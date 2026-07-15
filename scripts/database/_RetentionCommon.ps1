# _RetentionCommon.ps1 — shared functions for the backup retention tooling.
# This file defines ONLY functions (plus one dot-source of another
# functions-only file), so it can be dot-sourced from any script without
# clobbering caller variables.
# Read-only: nothing here deletes, moves, or rewrites any file.

# Get-MetadataIsolatedTestFlag — the single strict reader for isolatedTestBackup,
# shared with the backup publish path so both sides agree on what the field means.
. (Join-Path $PSScriptRoot '_BackupMetadata.ps1')

function Test-BackupRootGuard {
    [CmdletBinding()]
    param([string]$Path, [ref]$Reason)

    if ([string]::IsNullOrWhiteSpace($Path)) { $Reason.Value = 'path is empty'; return $false }
    if (-not [System.IO.Path]::IsPathRooted($Path)) { $Reason.Value = 'path is not absolute'; return $false }

    try { $full = [System.IO.Path]::GetFullPath($Path) } catch { $Reason.Value = 'path could not be normalized'; return $false }
    $normalized = $full.TrimEnd('\')

    if ($normalized -match '^[A-Za-z]:$') { $Reason.Value = 'refusing a drive root'; return $false }
    if ($normalized -match '^\\\\[^\\]+\\[^\\]+$') { $Reason.Value = 'refusing a UNC share root'; return $false }

    $repoRoot = ([System.IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..\..'))).TrimEnd('\')
    $forbidden = @($repoRoot)
    foreach ($envName in @('WINDIR', 'ProgramFiles', 'ProgramFiles(x86)', 'ProgramData', 'USERPROFILE', 'SystemRoot')) {
        $value = [System.Environment]::GetEnvironmentVariable($envName)
        if ($value) { $forbidden += ([System.IO.Path]::GetFullPath($value)).TrimEnd('\') }
    }
    $forbidden += 'C:\Users'

    foreach ($bad in $forbidden) {
        if ($normalized.Equals($bad, [System.StringComparison]::OrdinalIgnoreCase)) {
            $Reason.Value = "refusing a protected directory ($bad)"; return $false
        }
        if (($normalized + '\').StartsWith($bad + '\', [System.StringComparison]::OrdinalIgnoreCase)) {
            $Reason.Value = "refusing a path inside a protected directory ($bad)"; return $false
        }
    }

    # PostgreSQL install/data trees are recognized by their CONTENTS (PG_VERSION,
    # bin\postgres.exe), never by a directory merely being NAMED "PostgreSQL" —
    # the documented backup root D:\PJSK-Backups\PostgreSQL is named exactly that
    # and must be scannable. The path itself and every ancestor are probed, so
    # any location inside a real install or data tree stays refused.
    $probe = $normalized
    while ($probe -and ($probe -notmatch '^[A-Za-z]:$')) {
        if (Test-IsPostgresInstallOrDataDirectory -Path $probe) {
            $Reason.Value = "refusing a PostgreSQL install/data tree ($probe)"; return $false
        }
        $parent = [System.IO.Path]::GetDirectoryName($probe)
        if (-not $parent -or $parent -eq $probe) { break }
        $probe = $parent.TrimEnd('\')
    }

    if (-not (Test-Path -LiteralPath $normalized -PathType Container)) { $Reason.Value = 'directory does not exist (not auto-created)'; return $false }

    # An umbrella directory whose direct children are PostgreSQL installs or data
    # directories (e.g. D:\PostgreSQL holding 18\bin\postgres.exe) is refused too:
    # retention must never operate next to live database files.
    try {
        foreach ($child in (Get-ChildItem -LiteralPath $normalized -Directory -Force -ErrorAction Stop)) {
            if (Test-IsPostgresInstallOrDataDirectory -Path $child.FullName) {
                $Reason.Value = "refusing a directory that contains a PostgreSQL install/data tree ($($child.Name))"; return $false
            }
        }
    } catch {
        $Reason.Value = 'directory contents could not be inspected'; return $false
    }

    $item = Get-Item -LiteralPath $normalized -Force
    if (($item.Attributes -band [System.IO.FileAttributes]::ReparsePoint) -ne 0) {
        $Reason.Value = 'refusing a reparse point / junction / symlink'; return $false
    }

    $Reason.Value = $normalized
    return $true
}

# A real PostgreSQL directory is identified by definitive content markers, not
# by its name: every data directory carries a PG_VERSION file (even when the
# instance is stopped), and every install tree carries bin\postgres.exe. A probe
# failure (permissions etc.) reports no marker; the caller's existence and
# containment checks still apply.
function Test-IsPostgresInstallOrDataDirectory {
    [CmdletBinding()]
    param([Parameter(Mandatory)][string]$Path)

    # -ErrorAction SilentlyContinue: probing inside an ACL-protected data
    # directory raises Access denied; that must neither escape nor spam the
    # console. Such a directory is still refused — by the ancestor probe when it
    # sits inside an install tree, and otherwise by the caller's fail-closed
    # contents inspection.
    try {
        if (Test-Path -LiteralPath (Join-Path $Path 'PG_VERSION') -PathType Leaf -ErrorAction SilentlyContinue) { return $true }
        $bin = Join-Path $Path 'bin'
        foreach ($exe in @('postgres.exe', 'pg_ctl.exe', 'initdb.exe')) {
            if (Test-Path -LiteralPath (Join-Path $bin $exe) -PathType Leaf -ErrorAction SilentlyContinue) { return $true }
        }
    } catch { }
    return $false
}

function Test-IsReparsePoint([string]$Path) {
    try {
        $item = Get-Item -LiteralPath $Path -Force -ErrorAction Stop
        return (($item.Attributes -band [System.IO.FileAttributes]::ReparsePoint) -ne 0)
    } catch { return $false }
}

function Get-PostgresBackupRetentionReport {
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
        [switch]$VerifyHash
    )

    $reason = [ref]''
    if (-not (Test-BackupRootGuard -Path $BackupRoot -Reason $reason)) {
        throw "BackupRoot rejected: $($reason.Value)"
    }
    $root = $reason.Value
    $nowUtc = $Now.ToUniversalTime()

    $metadataFiles = @()
    try {
        $metadataFiles = Get-ChildItem -LiteralPath $root -Recurse -File -Filter '*.metadata.json' -ErrorAction Stop |
            Where-Object { -not (Test-IsReparsePoint $_.DirectoryName) }
    } catch {
        $metadataFiles = Get-ChildItem -LiteralPath $root -Recurse -File -Filter '*.metadata.json' -ErrorAction SilentlyContinue
    }

    $sets = @()
    foreach ($meta in $metadataFiles) {
        $dir = $meta.DirectoryName
        $base = $meta.Name.Substring(0, $meta.Name.Length - '.metadata.json'.Length)
        $dumpPath = Join-Path $dir ($base + '.dump')
        $validationPath = Join-Path $dir ($base + '.validation.json')
        $dumpPartial = Join-Path $dir ($base + '.dump.partial')
        $metaPartial = Join-Path $dir ($base + '.metadata.json.partial')

        $errorText = ''
        $status = 'unknown'
        $isolated = $null
        $createdUtc = $null
        $metaSha = ''
        $actualSha = ''
        $validationResult = 'none'
        $hasPartial = (Test-Path -LiteralPath $dumpPartial) -or (Test-Path -LiteralPath $metaPartial)
        $dumpExists = Test-Path -LiteralPath $dumpPath -PathType Leaf
        $isReparse = (Test-IsReparsePoint $dir) -or (Test-IsReparsePoint $meta.FullName) -or ($dumpExists -and (Test-IsReparsePoint $dumpPath))

        $metaObj = $null
        try {
            $metaObj = Get-Content -LiteralPath $meta.FullName -Raw -ErrorAction Stop | ConvertFrom-Json -ErrorAction Stop
        } catch {
            $errorText = 'metadata JSON unparseable'
        }
        $isolatedReason = ''
        if ($metaObj) {
            if ($metaObj.PSObject.Properties.Name -contains 'dumpSha256') { $metaSha = "$($metaObj.dumpSha256)" }
            # Strict: $null here means missing or not a real boolean. Never [bool]
            # this field — [bool]$null is $false (a missing flag would read as a
            # real backup and become deletable) and [bool]"false" is $true (a
            # stringly-typed flag would read as protected drill evidence).
            $isolated = Get-MetadataIsolatedTestFlag -Metadata $metaObj -Reason ([ref]$isolatedReason)
            if ($metaObj.PSObject.Properties.Name -contains 'createdAtUtc') {
                try { $createdUtc = ([datetime]$metaObj.createdAtUtc).ToUniversalTime() } catch { $createdUtc = $null }
            }
        }

        if (Test-Path -LiteralPath $validationPath -PathType Leaf) {
            try {
                $valObj = Get-Content -LiteralPath $validationPath -Raw -ErrorAction Stop | ConvertFrom-Json -ErrorAction Stop
                $ovr = "$($valObj.overallResult)"
                $valSha = "$($valObj.dumpSha256)"
                if ($ovr -eq 'passed' -and $metaSha -and $valSha -and ($valSha -ieq $metaSha)) { $validationResult = 'passed' }
                elseif ($ovr -eq 'passed') { $validationResult = 'passed-hash-mismatch' }
                else { $validationResult = 'failed' }
            } catch {
                $validationResult = 'unparseable'
                if (-not $errorText) { $errorText = 'validation JSON unparseable' }
            }
        }

        if ($VerifyHash -and $dumpExists -and -not $isReparse) {
            try { $actualSha = (Get-FileHash -LiteralPath $dumpPath -Algorithm SHA256 -ErrorAction Stop).Hash } catch { $actualSha = '' }
        }

        $validationPathValue = ''
        if (Test-Path -LiteralPath $validationPath -PathType Leaf) { $validationPathValue = $validationPath }

        $lastWrite = $null
        try { $lastWrite = (Get-Item -LiteralPath $meta.FullName -Force).LastWriteTimeUtc } catch {}
        $dumpSize = $null
        if ($dumpExists) { try { $dumpSize = (Get-Item -LiteralPath $dumpPath -Force).Length } catch {} }

        if ($isReparse) {
            $status = 'unknown'; if (-not $errorText) { $errorText = 'reparse point in set' }
        } elseif ($hasPartial) {
            $status = 'incomplete'; if (-not $errorText) { $errorText = 'partial file present' }
        } elseif (-not $dumpExists) {
            $status = 'orphan'; if (-not $errorText) { $errorText = 'metadata without dump' }
        } elseif (-not $metaObj) {
            $status = 'unknown'
        } elseif ($null -eq $isolated) {
            # Metadata whose backup mode cannot be trusted is never classified as
            # either a real backup or drill evidence: 'unknown' keeps it out of
            # the verified set (so it can never be auto-deleted) and the error
            # flags it as Decision=Error for a human to look at.
            $status = 'unknown'
            if (-not $errorText) { $errorText = "metadata $isolatedReason" }
        } elseif ($validationResult -eq 'failed' -or $validationResult -eq 'passed-hash-mismatch' -or $validationResult -eq 'unparseable') {
            $status = 'validation-failed'
        } elseif ($VerifyHash -and $metaSha -and $actualSha -and ($actualSha -ine $metaSha)) {
            $status = 'validation-failed'; if (-not $errorText) { $errorText = 'sha256 mismatch vs metadata' }
        } elseif ($validationResult -eq 'passed' -and $VerifyHash -and $metaSha -and $actualSha -and ($actualSha -ieq $metaSha)) {
            $status = 'verified'
        } else {
            $status = 'unverified'
        }

        $sets += [pscustomobject]@{
            SetId = $base; Directory = $dir; DumpPath = $dumpPath; MetadataPath = $meta.FullName
            ValidationPath = $validationPathValue; CreatedUtc = $createdUtc; LastWriteUtc = $lastWrite
            DumpSizeBytes = $dumpSize; MetadataSha256 = $metaSha; ActualSha256 = $actualSha
            ValidationResult = $validationResult; IsolatedTest = $isolated; HasPartial = $hasPartial
            IsReparsePoint = $isReparse; Status = $status; RetentionTier = ''; Decision = ''
            DecisionReason = ''; Error = $errorText
        }
    }

    # Lone dumps / validations without metadata -> orphan (always protected).
    $knownBases = @{}
    foreach ($s in $sets) { $knownBases[($s.Directory + '|' + $s.SetId)] = $true }
    $orphanCandidates = @()
    try {
        $orphanCandidates = Get-ChildItem -LiteralPath $root -Recurse -File -ErrorAction SilentlyContinue |
            Where-Object { $_.Name -like '*.dump' -or $_.Name -like '*.validation.json' }
    } catch {}
    foreach ($f in $orphanCandidates) {
        if (Test-IsReparsePoint $f.DirectoryName) { continue }
        $b = $null
        if ($f.Name -like '*.dump') { $b = $f.Name.Substring(0, $f.Name.Length - '.dump'.Length) }
        elseif ($f.Name -like '*.validation.json') { $b = $f.Name.Substring(0, $f.Name.Length - '.validation.json'.Length) }
        if (-not $b) { continue }
        if ($knownBases.ContainsKey(($f.DirectoryName + '|' + $b))) { continue }
        $knownBases[($f.DirectoryName + '|' + $b)] = $true
        $sets += [pscustomobject]@{
            SetId = $b; Directory = $f.DirectoryName; DumpPath = ''; MetadataPath = ''; ValidationPath = ''
            CreatedUtc = $null; LastWriteUtc = $f.LastWriteTimeUtc; DumpSizeBytes = $null
            MetadataSha256 = ''; ActualSha256 = ''; ValidationResult = 'none'; IsolatedTest = $null
            HasPartial = $false; IsReparsePoint = $false; Status = 'orphan'; RetentionTier = ''
            Decision = ''; DecisionReason = ''; Error = 'file without metadata anchor'
        }
    }

    $sets = $sets | Sort-Object @{ Expression = { if ($_.CreatedUtc) { $_.CreatedUtc } else { [datetime]::MaxValue } } }, SetId

    $verified = @($sets | Where-Object { $_.Status -eq 'verified' -and $_.IsolatedTest -ne $true -and $_.CreatedUtc })
    $verifiedNewestFirst = @($verified | Sort-Object CreatedUtc -Descending)

    $keepSetIds = New-Object System.Collections.Generic.HashSet[string]
    $tierOf = @{}

    if ($verifiedNewestFirst.Count -gt 0) {
        [void]$keepSetIds.Add($verifiedNewestFirst[0].SetId); $tierOf[$verifiedNewestFirst[0].SetId] = 'newest'
    }
    for ($i = 0; $i -lt [Math]::Min($MinimumVerifiedBackups, $verifiedNewestFirst.Count); $i++) {
        [void]$keepSetIds.Add($verifiedNewestFirst[$i].SetId)
        if (-not $tierOf.ContainsKey($verifiedNewestFirst[$i].SetId)) { $tierOf[$verifiedNewestFirst[$i].SetId] = 'min-count' }
    }
    foreach ($s in $verified) {
        if (($nowUtc - $s.CreatedUtc).TotalDays -le $KeepAllDays) {
            [void]$keepSetIds.Add($s.SetId); if (-not $tierOf.ContainsKey($s.SetId)) { $tierOf[$s.SetId] = 'keep-all' }
        }
    }
    $seenDay = @{}; $seenWeek = @{}; $seenMonth = @{}
    foreach ($s in $verifiedNewestFirst) {
        $ageDays = ($nowUtc - $s.CreatedUtc).TotalDays
        if ($ageDays -le $DailyDays) {
            $k = $s.CreatedUtc.ToString('yyyy-MM-dd')
            if (-not $seenDay.ContainsKey($k)) { $seenDay[$k] = $true; [void]$keepSetIds.Add($s.SetId); if (-not $tierOf.ContainsKey($s.SetId)) { $tierOf[$s.SetId] = 'daily' } }
        }
        if ($ageDays -le ($WeeklyWeeks * 7)) {
            $cal = [System.Globalization.CultureInfo]::InvariantCulture.Calendar
            $wk = "$($s.CreatedUtc.Year)-W$($cal.GetWeekOfYear($s.CreatedUtc, [System.Globalization.CalendarWeekRule]::FirstFourDayWeek, [System.DayOfWeek]::Monday))"
            if (-not $seenWeek.ContainsKey($wk)) { $seenWeek[$wk] = $true; [void]$keepSetIds.Add($s.SetId); if (-not $tierOf.ContainsKey($s.SetId)) { $tierOf[$s.SetId] = 'weekly' } }
        }
        if ($ageDays -le ($MonthlyMonths * 31)) {
            $mo = $s.CreatedUtc.ToString('yyyy-MM')
            if (-not $seenMonth.ContainsKey($mo)) { $seenMonth[$mo] = $true; [void]$keepSetIds.Add($s.SetId); if (-not $tierOf.ContainsKey($s.SetId)) { $tierOf[$s.SetId] = 'monthly' } }
        }
    }

    $protectNameSet = New-Object System.Collections.Generic.HashSet[string]([System.StringComparer]::OrdinalIgnoreCase)
    foreach ($n in $ProtectName) { [void]$protectNameSet.Add($n) }

    foreach ($s in $sets) {
        if ($s.Status -ne 'verified') {
            $s.Decision = 'Protected'
            $s.DecisionReason = "status=$($s.Status) (never auto-deleted)"
            if ($s.Error) { $s.Decision = 'Error' }
            continue
        }
        if ($s.IsolatedTest -eq $true) { $s.Decision = 'Protected'; $s.DecisionReason = 'isolated restore-drill evidence'; continue }
        if ($protectNameSet.Contains($s.SetId)) { $s.Decision = 'Protected'; $s.DecisionReason = 'in -ProtectName list'; continue }
        if ($s.LastWriteUtc -and (($nowUtc - $s.LastWriteUtc).TotalMinutes -lt $RecentChangeMinutes)) {
            $s.Decision = 'Protected'; $s.DecisionReason = 'recently modified (still changing?)'; continue
        }
        if ($keepSetIds.Contains($s.SetId)) {
            $s.RetentionTier = $tierOf[$s.SetId]
            $s.Decision = 'Keep'; $s.DecisionReason = "retention tier: $($tierOf[$s.SetId])"; continue
        }
        if (-not $VerifyHash) {
            $s.Decision = 'Protected'; $s.DecisionReason = 'hash not verified (-VerifyHash off)'; continue
        }
        $s.Decision = 'Candidate'; $s.DecisionReason = 'verified, outside all retention tiers'
    }

    return , $sets
}
