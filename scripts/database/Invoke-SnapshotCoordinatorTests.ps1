# Offline tests for Export-PostgresSnapshot.ps1. The PostgreSQL executables are
# local mocks; this suite never opens a network connection or touches a database.
[CmdletBinding()]
param([switch]$PostRenameOnly)

$ErrorActionPreference='Stop'
Set-StrictMode -Version 2.0
$scriptDir=$PSScriptRoot
$exporter=Join-Path $scriptDir 'Export-PostgresSnapshot.ps1'
. (Join-Path $scriptDir '_SnapshotCoordinator.ps1')
$script:pass=0; $script:fail=0

function Check([bool]$Condition,[string]$Name){
    if($Condition){Write-Output "PASS  $Name";$script:pass++}else{Write-Output "FAIL  $Name";$script:fail++}
}
function Throws([scriptblock]$Action,[string]$Name){
    $thrown=$false
    try{& $Action}catch{$thrown=$true}
    Check $thrown $Name
}
function ThrowsLike([scriptblock]$Action,[string]$Pattern,[string]$Name){
    $message=''
    try{& $Action}catch{$message=$_.Exception.Message}
    Check ($message -match $Pattern) $Name
}

$testRoot=Join-Path ([IO.Path]::GetTempPath()) ('pjsk-snapshot-tests-'+[guid]::NewGuid().ToString('N').Substring(0,10))
$outsideSentinel=Join-Path ([IO.Path]::GetTempPath()) ('pjsk-snapshot-outside-'+[guid]::NewGuid().ToString('N')+'.txt')
[IO.File]::WriteAllText($outsideSentinel,'outside-test-root-preserve')
New-Item -ItemType Directory -Path $testRoot | Out-Null
$oldPath=$env:PATH
try{
    $csc=Join-Path $env:WINDIR 'Microsoft.NET\Framework64\v4.0.30319\csc.exe'
    if(-not(Test-Path -LiteralPath $csc)){throw "C# compiler not found: $csc"}
    $mockBin=Join-Path $testRoot 'mock-bin'; New-Item -ItemType Directory -Path $mockBin | Out-Null

    $psqlSource=Join-Path $testRoot 'mock_psql.cs'
    Set-Content -LiteralPath $psqlSource -Encoding ascii -Value @'
using System;
class MockPsql {
  static int Main(string[] args) {
    foreach (var a in args) if (a == "--version") { Console.WriteLine("psql (PostgreSQL) 18.4"); return 0; }
    Console.Out.Flush();
    string fail = Environment.GetEnvironmentVariable("PJSK_MOCK_FAIL_STAGE") ?? "";
    string line;
    while ((line = Console.ReadLine()) != null) {
      if (line.StartsWith("\\echo ")) { Console.WriteLine(line.Substring(6)); Console.Out.Flush(); continue; }
      if (line == "\\q") return 0;
      if (line.Contains("PJSK:EXPORT_SNAPSHOT")) {
        if (fail == "export") { Console.Error.WriteLine("mock export failure"); return 7; }
        Console.WriteLine("00000003-0000001B-1");
      } else if (line.Contains("PJSK:SERVER_FACTS")) Console.WriteLine("pjsk_mock|18.4|180004|repeatable read|on");
      else if (line.Contains("PJSK:EXPORT_TIME")) Console.WriteLine("2026-07-19T02:00:00.000000Z");
      else if (line.Contains("PJSK:MIGRATIONS")) {
        if (fail == "migrations") { Console.Error.WriteLine("mock migration failure"); return 8; }
        Console.WriteLine("0005_import_history.sql"); Console.WriteLine("0005_product_series.sql");
      } else if (line.Contains("PJSK:TABLE_CATALOG")) {
        if (fail == "early_close") return 9;
        Console.WriteLine("schema_migrations"); Console.WriteLine("users");
      } else if (line.Contains("PJSK:TABLE_COUNT")) {
        if (fail == "baseline") { Console.Error.WriteLine("mock baseline failure"); return 10; }
        Console.WriteLine("2");
      } else if (line.Contains("PJSK:STATUS_CATALOG")) Console.WriteLine("users|status");
      else if (line.Contains("PJSK:STATUS_DISTRIBUTION")) Console.WriteLine("active|2");
      else if (line.Contains("PJSK:TIME_CATALOG")) { Console.WriteLine("import_batches|created_at"); Console.WriteLine("order_items|created_at"); Console.WriteLine("orders|created_at"); Console.WriteLine("payments|created_at"); Console.WriteLine("users|created_at"); }
      else if (line.Contains("PJSK:TIME_AGGREGATE")) Console.WriteLine("2|2|2026-01-01T00:00:00.000000Z|2026-01-02T00:00:00.000000Z");
      else if (line.Contains("PJSK:LEGACY_SUM")) Console.WriteLine("2|2|4");
      else if (line.Contains("PJSK:SOURCE_METADATA")) Console.WriteLine("100|1|1");
      else if (line.Contains("PJSK:SNAPSHOT_ALIVE")) Console.WriteLine("1");
      Console.Out.Flush();
    }
    return 0;
  }
}
'@
    $dumpSource=Join-Path $testRoot 'mock_pg_dump.cs'
    Set-Content -LiteralPath $dumpSource -Encoding ascii -Value @'
using System;
using System.IO;
class MockDump {
  static int Main(string[] args) {
    foreach(var a in args) if(a=="--version"){Console.WriteLine("pg_dump (PostgreSQL) 18.4");return 0;}
    string file=null; bool snapshot=false;
    for(int i=0;i<args.Length;i++){if(args[i]=="--file"&&i+1<args.Length)file=args[++i];else if(args[i].StartsWith("--snapshot="))snapshot=true;}
    if(!snapshot||file==null)return 11;
    string fail=Environment.GetEnvironmentVariable("PJSK_MOCK_FAIL_STAGE")??"";
    if(fail=="dump"){File.WriteAllText(file,"FORENSIC PARTIAL");return 12;}
    if(fail=="empty"){File.WriteAllBytes(file,new byte[0]);return 0;}
    File.WriteAllText(file,"MOCK CUSTOM ARCHIVE - NO DATABASE DATA");return 0;
  }
}
'@
    $restoreSource=Join-Path $testRoot 'mock_pg_restore.cs'
    Set-Content -LiteralPath $restoreSource -Encoding ascii -Value @'
using System;
class MockRestore {
  static int Main(string[] args) {
    foreach(var a in args) if(a=="--version"){Console.WriteLine("pg_restore (PostgreSQL) 18.4");return 0;}
    if((Environment.GetEnvironmentVariable("PJSK_MOCK_FAIL_STAGE")??"")=="restore")return 13;
    Console.WriteLine("; mock archive"); Console.WriteLine("1; 0 0 TABLE public users mock"); return 0;
  }
}
'@
    $gitSource=Join-Path $testRoot 'mock_git.cs'
    Set-Content -LiteralPath $gitSource -Encoding ascii -Value @'
using System;
using System.IO;
class MockGit {
  static int Main(string[] args) {
    string root=Environment.GetEnvironmentVariable("PJSK_MOCK_TAMPER_ROOT")??"";
    if(root.Length>0&&Directory.Exists(root)){
      foreach(string file in Directory.GetFiles(root,"*.dump.partial",SearchOption.AllDirectories)) File.AppendAllText(file,"SHA-MISMATCH-TAMPER");
    }
    Console.WriteLine("0123456789abcdef0123456789abcdef01234567");
    return 0;
  }
}
'@
    & $csc /nologo /target:exe ("/out:"+(Join-Path $mockBin 'psql.exe')) $psqlSource | Out-Null; $c1=$LASTEXITCODE
    & $csc /nologo /target:exe ("/out:"+(Join-Path $mockBin 'pg_dump.exe')) $dumpSource | Out-Null; $c2=$LASTEXITCODE
    & $csc /nologo /target:exe ("/out:"+(Join-Path $mockBin 'pg_restore.exe')) $restoreSource | Out-Null; $c3=$LASTEXITCODE
    & $csc /nologo /target:exe ("/out:"+(Join-Path $mockBin 'git.exe')) $gitSource | Out-Null; $c4=$LASTEXITCODE
    Check ($c1 -eq 0 -and $c2 -eq 0 -and $c3 -eq 0 -and $c4 -eq 0) 'mock PostgreSQL 18.4 tools and SHA tamper hook compiled'
    $env:PATH=$mockBin+';'+$oldPath

    $passfile=Join-Path $testRoot 'pgpass.conf'
    [IO.File]::WriteAllText($passfile,'127.0.0.1:5432:pjsk_mock:postgres:mock')
    $acl=New-Object Security.AccessControl.FileSecurity
    $acl.SetAccessRuleProtection($true,$false)
    $identity=[Security.Principal.WindowsIdentity]::GetCurrent().User
    $acl.SetOwner($identity)
    $rule=New-Object Security.AccessControl.FileSystemAccessRule($identity,'FullControl','Allow')
    $acl.AddAccessRule($rule)
    Set-Acl -LiteralPath $passfile -AclObject $acl
    $aclReason=''; Check (Test-PassfileAcl $passfile ([ref]$aclReason)) 'protected passfile ACL accepted without reading its content'
    $historyDirectory=Join-Path $testRoot 'history-sentinel';New-Item -ItemType Directory -Path $historyDirectory|Out-Null
    $historySentinel=Join-Path $historyDirectory 'sentinel.txt';[IO.File]::WriteAllText($historySentinel,'historical-final-preserve')
    $historySentinelHash=(Get-FileHash -LiteralPath $historySentinel -Algorithm SHA256).Hash

    $powershell=(Get-Command powershell.exe).Source
    function Invoke-MockExport([string]$Case,[string]$Failure='',[switch]$QuarantineFailure){
        $root=Join-Path $testRoot $Case; New-Item -ItemType Directory -Path $root | Out-Null
        $old=$env:PJSK_MOCK_FAIL_STAGE
        $oldSnapshotFailure=$env:PJSK_SNAPSHOT_TEST_FAIL_STAGE
        $oldTamperRoot=$env:PJSK_MOCK_TAMPER_ROOT
        $oldQuarantineFailure=$env:PJSK_SNAPSHOT_TEST_FAIL_QUARANTINE
        if($Failure){$env:PJSK_MOCK_FAIL_STAGE=$Failure}else{Remove-Item Env:\PJSK_MOCK_FAIL_STAGE -ErrorAction SilentlyContinue}
        if($Failure -like 'inject:*'){$env:PJSK_SNAPSHOT_TEST_FAIL_STAGE=$Failure.Substring(7);Remove-Item Env:\PJSK_MOCK_FAIL_STAGE -ErrorAction SilentlyContinue}else{Remove-Item Env:\PJSK_SNAPSHOT_TEST_FAIL_STAGE -ErrorAction SilentlyContinue}
        if($Failure -eq 'sha-mismatch'){$env:PJSK_MOCK_TAMPER_ROOT=$root;Remove-Item Env:\PJSK_MOCK_FAIL_STAGE -ErrorAction SilentlyContinue}else{Remove-Item Env:\PJSK_MOCK_TAMPER_ROOT -ErrorAction SilentlyContinue}
        if($QuarantineFailure){$env:PJSK_SNAPSHOT_TEST_FAIL_QUARANTINE='1'}else{Remove-Item Env:\PJSK_SNAPSHOT_TEST_FAIL_QUARANTINE -ErrorAction SilentlyContinue}
        $oldPref=$ErrorActionPreference;$ErrorActionPreference='Continue'
        try{
            $output=(& $powershell -NoProfile -NonInteractive -ExecutionPolicy Bypass -File $exporter -SourceHost 127.0.0.1 -SourcePort 5432 -SourceDatabase pjsk_mock -SourceUser postgres -OutputRoot $root -PgBinDirectory $mockBin -Mode rehearsal -Passfile $passfile 2>&1 | ForEach-Object{"$_"}) -join "`n"
            $code=$LASTEXITCODE
        }finally{
            $ErrorActionPreference=$oldPref
            if($null -eq $old){Remove-Item Env:\PJSK_MOCK_FAIL_STAGE -ErrorAction SilentlyContinue}else{$env:PJSK_MOCK_FAIL_STAGE=$old}
            if($null -eq $oldSnapshotFailure){Remove-Item Env:\PJSK_SNAPSHOT_TEST_FAIL_STAGE -ErrorAction SilentlyContinue}else{$env:PJSK_SNAPSHOT_TEST_FAIL_STAGE=$oldSnapshotFailure}
            if($null -eq $oldTamperRoot){Remove-Item Env:\PJSK_MOCK_TAMPER_ROOT -ErrorAction SilentlyContinue}else{$env:PJSK_MOCK_TAMPER_ROOT=$oldTamperRoot}
            if($null -eq $oldQuarantineFailure){Remove-Item Env:\PJSK_SNAPSHOT_TEST_FAIL_QUARANTINE -ErrorAction SilentlyContinue}else{$env:PJSK_SNAPSHOT_TEST_FAIL_QUARANTINE=$oldQuarantineFailure}
        }
        $dirs=@(Get-ChildItem -LiteralPath $root -Directory)
        [pscustomobject]@{
            Code=$code;Output=$output;Root=$root;Run=@($dirs|Select-Object -First 1)
            Final=@($dirs|Where-Object Name -match '^pjsk-rehearsal-\d{8}T\d{9}Z$')
            Failed=@($dirs|Where-Object Name -match '^pjsk-rehearsal-.*\.failed-')
            Staging=@($dirs|Where-Object Name -like '.pjsk-snapshot-staging-*')
        }
    }
    function Check-FailedExportEvidence($Result,[string]$Label){
        Check ($Result.Code -ne 0) "$Label returns nonzero"
        Check ($Result.Final.Count -eq 0) "$Label creates no final directory"
        Check ($Result.Staging.Count -eq 1) "$Label preserves exactly one staging evidence directory"
        $evidence=@($Result.Staging|Select-Object -First 1)
        Check ($evidence.Count -eq 1 -and $evidence[0].Name -match '(?i)(staging|partial|failed)') "$Label evidence directory has explicit failure semantics"
        $passPublished=$false
        if($evidence.Count -eq 1){$validationPath=Join-Path $evidence[0].FullName 'validation.json';if(Test-Path -LiteralPath $validationPath){try{$passPublished=((Get-Content -LiteralPath $validationPath -Raw|ConvertFrom-Json).verdict -eq 'PASS')}catch{$passPublished=$true}}}
        Check (-not $passPublished) "$Label publishes no validation PASS"
        $failedVerdictSafe=$true
        if($evidence.Count -eq 1){$failedPath=Join-Path $evidence[0].FullName 'validation.failed.json';if(Test-Path -LiteralPath $failedPath){try{$failedVerdictSafe=((Get-Content -LiteralPath $failedPath -Raw|ConvertFrom-Json).verdict -ne 'PASS')}catch{$failedVerdictSafe=$false}}}
        Check $failedVerdictSafe "$Label failure evidence verdict is never PASS"
        Check ((Get-FileHash -LiteralPath $historySentinel -Algorithm SHA256).Hash -eq $historySentinelHash) "$Label leaves the historical sentinel unchanged"
    }

    function Check-PostRenameQuarantine($Result,[string]$Label,[string]$ExpectedPreservedFile='',[switch]$ExpectStaging){
        Check ($Result.Code -ne 0) "$Label returns nonzero"
        Check ($Result.Output -notmatch 'Snapshot export PASS') "$Label never reports PASS"
        Check ($Result.Final.Count -eq 0) "$Label leaves no formally named final directory"
        Check ($Result.Failed.Count -eq 1) "$Label preserves exactly one failed quarantine directory"
        Check ($Result.Staging.Count -eq $(if($ExpectStaging){1}else{0})) "$Label preserves only the expected staging-path state"
        $failed=@($Result.Failed|Select-Object -First 1)
        $failedValidation=$null
        if($failed.Count -eq 1){
            $failedValidationPath=Join-Path $failed[0].FullName 'validation.failed.json'
            if(Test-Path -LiteralPath $failedValidationPath){try{$failedValidation=Get-Content -LiteralPath $failedValidationPath -Raw|ConvertFrom-Json}catch{}}
        }
        Check ($failed.Count -eq 1 -and -not(Test-Path -LiteralPath (Join-Path $failed[0].FullName 'validation.json'))) "$Label quarantines validation.json"
        Check ($failed.Count -eq 1 -and @(Get-ChildItem -LiteralPath $failed[0].FullName -File -Filter 'validation.prepublish-pass*.json').Count -eq 1) "$Label preserves the staged PASS only as prepublish evidence"
        Check ($null -ne $failedValidation -and $failedValidation.verdict -ne 'PASS' -and $failedValidation.publicationState -eq 'failed' -and $failedValidation.failedStage -eq 'post-rename-validation') "$Label records explicit post-rename non-PASS evidence"
        Check ($null -ne $failedValidation -and $failedValidation.productionRestoreAllowed -eq $false -and $failedValidation.successArtifactsPublished -eq $false) "$Label failure marker forbids production restore"
        if($ExpectedPreservedFile){Check ($failed.Count -eq 1 -and (Test-Path -LiteralPath (Join-Path $failed[0].FullName $ExpectedPreservedFile))) "$Label preserves the abnormal artifact for evidence"}
        if($ExpectStaging){Check ($Result.Staging.Count -eq 1 -and (Test-Path -LiteralPath (Join-Path $Result.Staging[0].FullName 'unknown-staging-evidence.txt'))) "$Label does not delete unknown staging evidence"}
        Check ((Get-FileHash -LiteralPath $historySentinel -Algorithm SHA256).Hash -eq $historySentinelHash) "$Label leaves historical final evidence unchanged"
    }

    function Invoke-PostRenameEvidenceTests {
        $postMissing=Invoke-MockExport 'post-rename-observation-missing' 'inject:post-rename-final-missing'
        Check-PostRenameQuarantine $postMissing 'post-rename final observation failure'

        $postStaging=Invoke-MockExport 'post-rename-staging-remains' 'inject:post-rename-staging-remains'
        Check-PostRenameQuarantine $postStaging 'post-rename staging-remains failure' -ExpectStaging

        $postExtra=Invoke-MockExport 'post-rename-extra-file' 'inject:post-rename-extra-file'
        Check-PostRenameQuarantine $postExtra 'post-rename strict artifact failure' 'unexpected.txt'

        $validationUpdate=Invoke-MockExport 'validation-publish-update-failure' 'inject:validation-publish-update'
        Check ($validationUpdate.Code -ne 0 -and $validationUpdate.Output -match 'validation-publication') 'validation published-state update failure is attributed to its exact gate'
        Check ($validationUpdate.Final.Count -eq 0 -and $validationUpdate.Failed.Count -eq 1) 'validation update failure is quarantined with no final directory'
        $validationFailed=@($validationUpdate.Failed|Select-Object -First 1)
        Check ($validationFailed.Count -eq 1 -and -not(Test-Path -LiteralPath (Join-Path $validationFailed[0].FullName 'validation.json'))) 'validation update failure exposes no official PASS validation'
        Check ($validationFailed.Count -eq 1 -and (Test-Path -LiteralPath (Join-Path $validationFailed[0].FullName 'validation.json.publish.partial'))) 'validation update failure preserves its partial evidence'

        $quarantineFail=Invoke-MockExport 'quarantine-rename-failure' 'inject:post-rename-extra-file' -QuarantineFailure
        $quarantineOutputNormalized=($quarantineFail.Output -replace '\s+',''); $quarantineStatusSafe=($quarantineFail.Code -ne 0 -and $quarantineOutputNormalized -match '(?i)quarantinefailed' -and $quarantineOutputNormalized -match '(?i)manualisolation')
        Check $quarantineStatusSafe 'quarantine rename failure reports explicit manual-isolation status'
        Check ($quarantineFail.Final.Count -eq 1 -and $quarantineFail.Failed.Count -eq 0) 'quarantine rename failure truthfully preserves the original final path'
        $remainingFinal=@($quarantineFail.Final|Select-Object -First 1)
        Check ($remainingFinal.Count -eq 1 -and -not(Test-Path -LiteralPath (Join-Path $remainingFinal[0].FullName 'validation.json'))) 'quarantine rename failure isolates official PASS validation in place'
        $remainingFailure=$null
        if($remainingFinal.Count -eq 1){$p=Join-Path $remainingFinal[0].FullName 'validation.failed.json';if(Test-Path -LiteralPath $p){$remainingFailure=Get-Content -LiteralPath $p -Raw|ConvertFrom-Json}}
        Check ($null -ne $remainingFailure -and $remainingFailure.verdict -ne 'PASS' -and $remainingFailure.productionRestoreAllowed -eq $false) 'quarantine rename failure leaves an explicit non-PASS marker when possible'
        Check ($quarantineFail.Output -notmatch 'Snapshot export PASS') 'quarantine rename failure never reports PASS'
        Check ((Get-FileHash -LiteralPath $historySentinel -Algorithm SHA256).Hash -eq $historySentinelHash) 'all post-rename failure injections preserve historical evidence'

        $postSuccess=Invoke-MockExport 'post-rename-success'
        Check ($postSuccess.Code -eq 0 -and $postSuccess.Output -match 'Snapshot export PASS') 'normal post-rename path reports success'
        Check ($postSuccess.Final.Count -eq 1 -and $postSuccess.Staging.Count -eq 0 -and $postSuccess.Failed.Count -eq 0) 'normal post-rename path has one final and no staging or failed directory'
        $successFinal=@($postSuccess.Final|Select-Object -First 1)
        $successFiles=if($successFinal.Count -eq 1){@(Get-ChildItem -LiteralPath $successFinal[0].FullName -File)}else{@()}
        Check ($successFiles.Count -eq 6 -and @(Get-ChildItem -LiteralPath $successFinal[0].FullName -Directory).Count -eq 0) 'normal post-rename path has the strict six-file set'
        Check (@($successFiles|Where-Object Name -like '*.partial').Count -eq 0) 'normal post-rename path has no partial artifact'
        $successValidation=$null
        if($successFinal.Count -eq 1){$successValidation=Get-Content -LiteralPath (Join-Path $successFinal[0].FullName 'validation.json') -Raw|ConvertFrom-Json}
        Check ($null -ne $successValidation -and $successValidation.verdict -eq 'PASS' -and $successValidation.publicationState -eq 'published') 'normal post-rename validation is explicitly published PASS'
    }

    if($PostRenameOnly){
        Invoke-PostRenameEvidenceTests
    }else{
    $ok=Invoke-MockExport 'success'
    Check ($ok.Code -eq 0) 'successful coordinator returns zero'
    if($ok.Code -ne 0){Write-Output ("MOCK SUCCESS DIAGNOSTIC: "+$ok.Output);throw 'mock success path failed'}
    $files=@(Get-ChildItem -LiteralPath $ok.Run.FullName -File)
    Check ($files.Count -eq 6) 'success publishes exactly six artifacts'
    Check ($ok.Final.Count -eq 1 -and $ok.Staging.Count -eq 0) 'success publishes one final directory and removes the staging name'
    Check (@($files|Where-Object Name -like '*.partial').Count -eq 0) 'success rejects residual partial files'
    $meta=Get-Content -LiteralPath (Join-Path $ok.Run.FullName 'export-metadata.json') -Raw | ConvertFrom-Json
    $validation=Get-Content -LiteralPath (Join-Path $ok.Run.FullName 'validation.json') -Raw | ConvertFrom-Json
    $baseline=Get-Content -LiteralPath (Join-Path $ok.Run.FullName 'source-baseline.json') -Raw | ConvertFrom-Json
    Check ($meta.productionRestoreAllowed -eq $false -and $validation.verdict -eq 'PASS' -and $validation.publicationState -eq 'published') 'rehearsal is marked non-production and published PASS only after all gates'
    Check ($meta.snapshotCoordinatorRunId -eq $baseline.snapshotCoordinatorRunId -and $validation.snapshotConsistency.coordinatorRunId -eq $meta.snapshotCoordinatorRunId) 'baseline and metadata carry one coordinator run binding'
    Check (@($baseline.businessInvariants.counts.PSObject.Properties).Count -eq 23 -and @($baseline.businessInvariants.sums.PSObject.Properties).Count -eq 10 -and @($baseline.businessInvariants.time_bounds_utc.PSObject.Properties).Count -eq 5 -and @($baseline.businessInvariants.status_distributions.PSObject.Properties).Count -eq 12 -and @($baseline.businessInvariants.binary_nonempty_counts.PSObject.Properties).Count -eq 3) 'schemaVersion 2 preserves every schemaVersion 1 business-invariant key'
    Check ((Get-Content -LiteralPath (Join-Path $ok.Run.FullName 'source-migrations.txt')).Count -eq 2) 'full migration list is published, not only count and max'

    $exportFail=Invoke-MockExport 'fail-export' 'export'; Check-FailedExportEvidence $exportFail 'snapshot failure'
    $baselineFail=Invoke-MockExport 'fail-baseline' 'baseline'; Check-FailedExportEvidence $baselineFail 'baseline failure'
    $migrationFail=Invoke-MockExport 'fail-migrations' 'migrations'; Check-FailedExportEvidence $migrationFail 'migrations failure'
    $dumpFail=Invoke-MockExport 'fail-dump' 'dump'; Check ($dumpFail.Code -ne 0) 'pg_dump failure returns nonzero'
    Check (@(Get-ChildItem -LiteralPath $dumpFail.Run.FullName -File -Filter '*.partial').Count -gt 0) 'failed run preserves forensic partial instead of publishing success'
    Check ($dumpFail.Final.Count -eq 0 -and $dumpFail.Staging.Count -eq 1) 'first artifact failure leaves staging evidence and no final directory'
    Check ((Get-Content -LiteralPath (Join-Path $dumpFail.Run.FullName 'validation.failed.json') -Raw | ConvertFrom-Json).verdict -eq 'FAIL') 'failed run records non-PASS verdict'
    $emptyFail=Invoke-MockExport 'fail-empty' 'empty'; Check ($emptyFail.Code -ne 0) 'empty dump is rejected'
    $restoreFail=Invoke-MockExport 'fail-restore' 'restore'; Check ($restoreFail.Code -ne 0) 'pg_restore --list failure is rejected'
    $earlyFail=Invoke-MockExport 'fail-early-close' 'early_close'; Check ($earlyFail.Code -ne 0) 'coordinator transaction ending early is rejected'
    $shaFail=Invoke-MockExport 'fail-sha-mismatch' 'sha-mismatch'
    Check-FailedExportEvidence $shaFail 'exporter SHA mismatch'
    $shaGateSafe=($shaFail.Output -match 'SHA-256 read\s*back verification failed' -and $shaFail.Output -notmatch 'Snapshot export PASS')
    if(-not $shaGateSafe){Write-Output "SHA MISMATCH DIAGNOSTIC: $($shaFail.Output)"}
    Check $shaGateSafe 'exporter SHA mismatch fails at the SHA readback gate and never reports PASS'

    Invoke-PostRenameEvidenceTests

    foreach($stage in @('migrations-written','baseline-built','baseline-written','metadata-written','validation-written','staging-finalized','pre-publication','directory-rename')){
        $caseName='fail-'+($stage -replace 'final','complete')
        $mid=Invoke-MockExport $caseName ("inject:"+$stage)
        $midSafe=($mid.Code -ne 0 -and $mid.Final.Count -eq 0 -and $mid.Staging.Count -eq 1)
        if(-not $midSafe){Write-Output "DIAGNOSTIC $stage code=$($mid.Code) final=$($mid.Final.Count) staging=$($mid.Staging.Count) output=$($mid.Output)"}
        Check $midSafe "failure at $stage preserves staging evidence and publishes no final directory"
    }

    $shaFile=Join-Path $testRoot 'sha-test.bin';[IO.File]::WriteAllText($shaFile,'abc')
    Check (-not(Test-ExpectedSha256 $shaFile ('0'*64))) 'SHA-256 mismatch is rejected'
    $source=[IO.File]::ReadAllText($exporter)
    Check ($source -match [regex]::Escape('"--snapshot=$snapshotId"')) 'pg_dump argument construction passes the exported snapshot ID'
    Check ($source -notmatch '\\\"C\\\"' -and $source -match 'collate\s+`"C`"') 'PowerShell SQL uses native double-quote escaping, never literal backslash commands'
    Throws {Assert-SnapshotMode -Mode cutover} 'cutover without write-freeze confirmation and window is rejected'
    Throws {Assert-ArtifactBaseName rehearsal 'pjsk-rehearsal-final-20260719T020000000Z'} 'rehearsal final/cutover naming is rejected'
    foreach($bad in @('C:\PJSK-cutover-artifacts','C:\PJSK-FINAL-artifacts','C:\temp\cutover\rehearsal','C:\temp\Final_Output\rehearsal')){
        ThrowsLike {Resolve-SnapshotOutputRoot $bad 'D:\pjsk' rehearsal} 'forbidden formal-release semantics' "rehearsal rejects normalized formal path $bad"
    }
    $relativeBad=Join-Path 'cutover' '..\cutover\rehearsal'
    ThrowsLike {Resolve-SnapshotOutputRoot $relativeBad 'D:\pjsk' rehearsal} 'forbidden formal-release semantics' 'rehearsal normalizes a relative path before rejecting cutover semantics'
    Check ((Resolve-SnapshotOutputRoot 'C:\PJSK-Snapshot-Rehearsal' 'D:\pjsk' rehearsal) -eq 'C:\PJSK-Snapshot-Rehearsal') 'rehearsal accepts snapshot-rehearsal output root'
    Check ((Resolve-SnapshotOutputRoot 'C:\PJSK-Rehearsal-Artifacts\' 'D:\pjsk' rehearsal) -eq 'C:\PJSK-Rehearsal-Artifacts') 'rehearsal accepts and normalizes a safe trailing-slash root'
    Check ((Resolve-SnapshotOutputRoot 'C:\PJSK-cutover-artifacts' 'D:\pjsk' cutover) -eq 'C:\PJSK-cutover-artifacts') 'cutover mode is not rejected by rehearsal path semantics'
    $existing=Join-Path $testRoot 'exists';New-Item -ItemType Directory -Path $existing|Out-Null
    Throws {Assert-NewOutputDirectory $existing} 'pre-existing output directory is rejected without overwrite'
    Throws {$null=Get-CanonicalJson ([ordered]@{amount=[double]1.25})} 'floating-point monetary JSON is rejected'
    $expected=@('0001_a.sql','0002_b.sql','0003_c.sql');$actual=@('0001_a.sql','0002_x.sql','0003_c.sql')
    Check (-not(Test-MigrationSetExactMatch $expected $actual)) 'same count/max with a different migration set is rejected'
    $scanReason='';Check (-not(Test-SnapshotSensitiveText 'DATABASE_URL=postgres://example' ([ref]$scanReason))) 'credential-like artifact content is rejected'
    Check ((Get-PostgresVersionInfo 'pg_dump (PostgreSQL) 18.4' 'pg_dump').Version -eq '18.4') 'exact PostgreSQL 18.4 tool is accepted'
    foreach($badVersion in @('18.3','18.5','18')){Throws {$null=Get-PostgresVersionInfo "pg_dump (PostgreSQL) $badVersion" 'pg_dump'} "PostgreSQL tool version $badVersion is rejected"}
    Throws {$null=Get-PostgresVersionInfo 'pg_dump (PostgreSQL) 18.4' 'pg_dump';$null=Get-PostgresVersionInfo 'pg_restore (PostgreSQL) 18.3' 'pg_restore';$null=Get-PostgresVersionInfo 'psql (PostgreSQL) 18.4' 'psql'} 'an inconsistent three-tool version set is rejected at the mismatched tool'
    Throws {$null=Get-PostgresVersionInfo 'pg_dump version unknown' 'pg_dump'} 'unparseable PostgreSQL tool version is rejected'
    Assert-PostgresServerVersion '18.4' '180004'; Check $true 'exact PostgreSQL 18.4 server version is accepted'
    foreach($serverCase in @(@('18.3','180003'),@('18.5','180005'),@('18.4','180003'),@('18.3','180004'),@('18','180000'))){Throws {Assert-PostgresServerVersion $serverCase[0] $serverCase[1]} "PostgreSQL server version $($serverCase[0])/$($serverCase[1]) is rejected"}
    $sourceMoves=@([regex]::Matches($source,'Move-Item\s+-LiteralPath\s+\$stagingDirectory\s+-Destination\s+\$finalDirectory'))
    Check ($sourceMoves.Count -eq 1) 'successful publication contains exactly one staging-to-final directory rename'
    $publishedAssignments=@([regex]::Matches($source,'\$published\s*=\s*\$true'))
    Check ($publishedAssignments.Count -eq 1 -and $publishedAssignments[0].Index -gt $source.IndexOf("Assert-ArtifactPublicationState `$finalDirectory 'published'")) 'published becomes true exactly once and only after published-final validation'
    Check ($source -match '\$directoryRenameCompleted\s*=\s*\$true' -and $source -match 'if \(-not \$published -and \$directoryRenameCompleted\)') 'rename completion and publication completion are separate states'
    Check ($source -notmatch '(?is)Remove-Item[^\r\n]*-Recurse') 'exporter failure handling never recursively deletes evidence'
    Throws {Assert-AtomicPublicationPaths 'C:\one\.pjsk-snapshot-staging-0123456789abcdef0123456789abcdef' 'C:\two\pjsk-rehearsal-20260719T020000000Z'} 'staging and final directories with different parents are rejected'
    Throws {Assert-AtomicPublicationPaths 'C:\one\.pjsk-snapshot-staging-0123456789abcdef0123456789abcdef' 'D:\one\pjsk-rehearsal-20260719T020000000Z'} 'staging and final directories on different volumes are rejected'
    $artifactFixture=Join-Path $testRoot '.pjsk-snapshot-staging-fedcba9876543210fedcba9876543210'
    New-Item -ItemType Directory -Path $artifactFixture|Out-Null
    $fixtureBase='pjsk-rehearsal-20260719T020000000Z'
    $fixtureDump=Join-Path $artifactFixture ($fixtureBase+'.dump')
    [IO.File]::WriteAllText($fixtureDump,'MOCK CUSTOM ARCHIVE')
    $fixtureHash=(Get-FileHash -LiteralPath $fixtureDump -Algorithm SHA256).Hash
    [IO.File]::WriteAllText(($fixtureDump+'.sha256'),"$fixtureHash  $fixtureBase.dump`n")
    [IO.File]::WriteAllText((Join-Path $artifactFixture 'source-baseline.json'),'{}')
    [IO.File]::WriteAllText((Join-Path $artifactFixture 'export-metadata.json'),'{}')
    [IO.File]::WriteAllText((Join-Path $artifactFixture 'validation.json'),'{"verdict":"PASS"}')
    [IO.File]::WriteAllText((Join-Path $artifactFixture 'source-migrations.txt'),"0001_fixture.sql`n")
    Assert-StagingArtifactSet $artifactFixture $fixtureBase;Check $true 'complete six-artifact staging set is accepted'
    Remove-Item -LiteralPath (Join-Path $artifactFixture 'source-baseline.json')
    Throws {Assert-StagingArtifactSet $artifactFixture $fixtureBase} 'staging set missing one required artifact is rejected'
    [IO.File]::WriteAllText((Join-Path $artifactFixture 'source-baseline.json'),'{}')
    [IO.File]::WriteAllText((Join-Path $artifactFixture 'leftover.partial'),'forensic')
    Throws {Assert-StagingArtifactSet $artifactFixture $fixtureBase} 'staging set with a residual partial is rejected'
    Remove-Item -LiteralPath (Join-Path $artifactFixture 'leftover.partial')
    [IO.File]::WriteAllText((Join-Path $artifactFixture 'validation.json'),'{"verdict":"FAIL"}')
    Throws {Assert-StagingArtifactSet $artifactFixture $fixtureBase} 'staging set with a non-PASS validation is rejected'
    [IO.File]::WriteAllText((Join-Path $artifactFixture 'validation.json'),'{"verdict":"PASS"}')
    $artifactFinal=Join-Path $testRoot 'pjsk-rehearsal-20260719T020000000Z'
    $unexpectedFile=Join-Path $artifactFixture 'unexpected.txt';[IO.File]::WriteAllText($unexpectedFile,'unexpected but preserved')
    $extraFileMessage='';try{Assert-StagingArtifactSet $artifactFixture $fixtureBase}catch{$extraFileMessage=$_.Exception.Message}
    Check ($extraFileMessage -match 'exactly six files') 'staging set with an extra ordinary file is explicitly rejected'
    Check (-not(Test-Path -LiteralPath $artifactFinal) -and (Test-Path -LiteralPath $unexpectedFile) -and @(Get-ChildItem -LiteralPath $artifactFixture -File).Count -eq 7) 'extra-file rejection preserves staging evidence and creates no final directory'
    Remove-Item -LiteralPath $unexpectedFile -Force
    $unexpectedDirectory=Join-Path $artifactFixture 'unexpected';New-Item -ItemType Directory -Path $unexpectedDirectory|Out-Null
    $extraDirectoryMessage='';try{Assert-StagingArtifactSet $artifactFixture $fixtureBase}catch{$extraDirectoryMessage=$_.Exception.Message}
    Check ($extraDirectoryMessage -match 'no subdirectories') 'staging set with an unexpected subdirectory remains explicitly rejected'
    Check (-not(Test-Path -LiteralPath $artifactFinal) -and (Test-Path -LiteralPath $unexpectedDirectory) -and @(Get-ChildItem -LiteralPath $artifactFixture -File).Count -eq 6) 'extra-subdirectory rejection preserves staging evidence and creates no final directory'
    Remove-Item -LiteralPath $unexpectedDirectory -Force
    $collision=Join-Path $testRoot 'existing-final';New-Item -ItemType Directory -Path $collision|Out-Null
    $sentinel=Join-Path $collision 'sentinel.txt';[IO.File]::WriteAllText($sentinel,'preserve')
    $sentinelHash=(Get-FileHash -LiteralPath $sentinel -Algorithm SHA256).Hash
    Throws {Assert-NewOutputDirectory $collision} 'pre-existing final directory is rejected before generation'
    Check ((Get-FileHash -LiteralPath $sentinel -Algorithm SHA256).Hash -eq $sentinelHash) 'pre-existing final directory content remains unchanged'
    Check ($dumpFail.Code -ne 0 -and $dumpFail.Output -notmatch 'Snapshot export PASS') 'external command failure cannot be reported as success'
    }
}finally{
    if($null -ne $oldPath){$env:PATH=$oldPath}
    if(Test-Path -LiteralPath $testRoot){Remove-Item -LiteralPath $testRoot -Recurse -Force -Confirm:$false}
}

Check (Test-Path -LiteralPath $outsideSentinel -PathType Leaf) 'test cleanup leaves the outside sentinel untouched'
if(Test-Path -LiteralPath $outsideSentinel){Remove-Item -LiteralPath $outsideSentinel -Force}

if($script:fail){Write-Output "RESULT: $script:fail failure(s), $script:pass passed";exit 1}
Write-Output "RESULT: all $script:pass snapshot coordinator tests passed"
exit 0
