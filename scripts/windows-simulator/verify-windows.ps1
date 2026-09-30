[CmdletBinding()]
param(
    [ValidateSet('Check', 'Copy', 'Job')]
    [string]$Stage = 'Check',
    [string]$TitleRegex,
    [string]$InitialLink,
    [ValidateSet('current', 'menu', 'direct')]
    [string]$Mode = 'menu',
    [string]$MoreMenuPoint,
    [string]$CopyLinkPoint,
    [string]$BaseUrl = 'http://127.0.0.1:19090',
    [string]$AuthToken,
    [ValidateRange(10, 600)]
    [int]$TimeoutSec = 180
)

# Run from any directory. Do not change execution policy, install packages,
# restart an existing bridge, or change its production configuration.
Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
if ($env:OS -ne 'Windows_NT') {
    throw 'Run this script on the Windows machine that displays WeChat.'
}
$packageRoot = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '../..'))
$savedEnv = @{}
Get-ChildItem Env: | Where-Object {
    $_.Name -like 'WECHAT_SIM_*' -or $_.Name -in @('PYTHONUTF8', 'PYTHONIOENCODING')
} | ForEach-Object { $savedEnv[$_.Name] = $_.Value }
$savedOutputEncoding = $OutputEncoding
$savedConsoleEncoding = [Console]::OutputEncoding
$report = [ordered]@{
    stage = $Stage
    startedAt = [DateTime]::UtcNow.ToString('o')
    passed = $false
}
$exitCode = 1

try {
    $OutputEncoding = New-Object System.Text.UTF8Encoding($false)
    [Console]::OutputEncoding = $OutputEncoding
    $env:PYTHONUTF8 = '1'
    $env:PYTHONIOENCODING = 'utf-8'

    if ($Stage -eq 'Job') {
        $uri = [Uri]$BaseUrl
        if (-not $uri.IsLoopback -or $uri.Scheme -notin @('http', 'https')) {
            throw 'This verification script accepts a local bridge URL only.'
        }
        if ([string]::IsNullOrWhiteSpace($InitialLink)) {
            $InitialLink = Read-Host 'Paste a real seed article/profile URL'
        }
        $headers = @{}
        if ($AuthToken) { $headers['Authorization'] = 'Bearer ' + $AuthToken }
        $base = $BaseUrl.TrimEnd('/')
        $status = Invoke-RestMethod -Uri "$base/v1/rpc/simulator-status" -Headers $headers -TimeoutSec 10
        if (-not $status.data.enabled) { throw 'Enable and calibrate the simulator, then restart bridge first.' }
        Write-Host 'This submits one real UI job to the existing bridge. Keep the Windows desktop unlocked.'
        Write-Host 'Before running: bridge skip_article_nav must be false, and the seed URL must open in the calibrated app.'
        $body = @{
            accounts = @(@{ id = 'windows-verification'; initialLink = $InitialLink })
            maxRetries = 1
            accountTimeoutSec = 90
        } | ConvertTo-Json -Depth 5 -Compress
        $created = Invoke-RestMethod -Uri "$base/v1/rpc/latest-article-link-jobs" `
            -Method Post -Headers $headers -ContentType 'application/json; charset=utf-8' `
            -Body ([Text.Encoding]::UTF8.GetBytes($body)) -TimeoutSec 10
        $jobId = $created.data.jobId
        $report['jobId'] = $jobId
        Write-Host "jobId=$jobId"
        $deadline = [DateTime]::UtcNow.AddSeconds($TimeoutSec)
        do {
            $job = Invoke-RestMethod -Uri "$base/v1/rpc/latest-article-link-jobs/$jobId" -Headers $headers -TimeoutSec 10
            if ($job.data.state -in @('completed', 'paused', 'stopped', 'failed')) { break }
            Start-Sleep -Seconds 2
        } while ([DateTime]::UtcNow -lt $deadline)
        $result = Invoke-RestMethod -Uri "$base/v1/rpc/latest-article-links?jobId=$jobId" -Headers $headers -TimeoutSec 10
        $report['job'] = $job.data
        $report['result'] = $result.data
        $result | ConvertTo-Json -Depth 10 | Write-Output
        if ($job.data.state -eq 'completed' -and $result.data.success -eq 1 -and $result.data.failed -eq 0) {
            $report.passed = $true
            $exitCode = 0
            Write-Host 'PASS: bridge returned one new link. Manually compare its title/date with the newest article in WeChat.'
        } elseif ($job.data.state -eq 'completed' -and $result.data.duplicate -eq 1 -and $result.data.failed -eq 0) {
            $report['dedupePassed'] = $true
            Write-Host 'DUPLICATE: the existing-link check worked; this run returned no new link.'
            $exitCode = 2
        } else {
            Write-Host "NOT PASSED: state=$($job.data.state). See the report for the reason."
            if ($job.data.state -notin @('completed', 'paused', 'stopped', 'failed')) {
                Write-Host 'Polling timed out; the job may still be running. Use its pause/stop endpoint if needed.'
            }
        }
    } else {
        $python = Get-Command py -ErrorAction SilentlyContinue
        if ($null -eq $python) { $python = Get-Command python -ErrorAction SilentlyContinue }
        if ($null -eq $python) { throw 'Python was not found. Install Python 3.10+ and reopen PowerShell.' }
        $pythonExe = $python.Source
        & $pythonExe -c 'import sys; assert sys.version_info >= (3,10), "Python 3.10+ required"; import pyautogui, pyperclip, pywinauto; print(sys.version)'
        if ($LASTEXITCODE -ne 0) {
            throw "Python/dependency check failed. Run: py -m pip install -r `"$PSScriptRoot/requirements.txt`""
        }
        if ($Stage -eq 'Check') {
            # Return only visible titled windows, so an operator can select
            # the article window instead of a similarly named chat window.
            $windows = & $pythonExe -c 'import json; from pywinauto import Desktop; print(json.dumps([{"handle": w.handle, "title": w.window_text()} for w in Desktop(backend="uia").windows(visible_only=True) if w.window_text()], ensure_ascii=True))'
            if ($LASTEXITCODE -ne 0) { throw 'Could not enumerate Windows desktop windows.' }
            $report['windows'] = ($windows -join "`n") | ConvertFrom-Json
            $report.windows | Format-Table -AutoSize | Out-Host
            $report.passed = $true
            $exitCode = 0
            Write-Host 'PASS: dependencies and visible-window enumeration. No article was collected.'
        } else {
            if ([string]::IsNullOrWhiteSpace($TitleRegex) -or $TitleRegex -eq '.*') {
                throw 'Pass -TitleRegex matching only the article window shown by -Stage Check.'
            }
            if ([string]::IsNullOrWhiteSpace($InitialLink)) {
                $InitialLink = Read-Host 'Paste the URL of the real article you manually opened'
            }
            if ($Mode -eq 'menu' -and (-not $MoreMenuPoint -or -not $CopyLinkPoint)) {
                throw 'Menu mode requires -MoreMenuPoint x,y and -CopyLinkPoint x,y (outer-window coordinates).'
            }
            # Isolate this smoke run from old calibration/launch/send settings.
            Get-ChildItem Env: | Where-Object { $_.Name -like 'WECHAT_SIM_*' } |
                ForEach-Object { [Environment]::SetEnvironmentVariable($_.Name, $null, 'Process') }
            $env:WECHAT_SIM_WINDOW_TITLE_REGEX = $TitleRegex
            $env:WECHAT_SIM_FLOW = 'auto'
            $env:WECHAT_SIM_SKIP_ARTICLE_NAV = '1'
            $env:WECHAT_SIM_LINK_MODE = $Mode
            $env:WECHAT_SIM_WAIT_SEC = '1'
            $env:WECHAT_SIM_WINDOW_TIMEOUT_SEC = '10'
            $env:WECHAT_SIM_CLIPBOARD_TIMEOUT_SEC = '3'
            $env:WECHAT_SIM_MORE_MENU_POINT = $MoreMenuPoint
            $env:WECHAT_SIM_COPY_LINK_POINT = $CopyLinkPoint
            $env:WECHAT_SIM_SEND_TO_FILE_TRANSFER = '0'
            Write-Host 'Keep the article visible with no text selected. Hands off the mouse/keyboard until the result appears.'
            $request = @{ id = 'windows-copy-check'; initialLink = $InitialLink } | ConvertTo-Json -Compress
            $raw = $request | & $pythonExe (Join-Path $PSScriptRoot 'latest_link_helper.py')
            if ($LASTEXITCODE -ne 0) { throw 'Helper process failed.' }
            $result = ($raw -join "`n") | ConvertFrom-Json
            $report['result'] = $result
            $result | ConvertTo-Json -Depth 5 | Write-Output
            if ($result.ok) {
                $report.passed = $true
                $exitCode = 0
                Write-Host 'PASS: current article copy only. Latest-article navigation and batch collection are NOT verified.'
            } else {
                Write-Host 'NOT PASSED: check the returned code/error. Ctrl+C support depends on the Windows WeChat version; use menu if needed.'
            }
        }
    }
} catch {
    $report['error'] = $_.Exception.Message
    Write-Host ("FAILED: " + $_.Exception.Message) -ForegroundColor Red
} finally {
    Get-ChildItem Env: | Where-Object {
        $_.Name -like 'WECHAT_SIM_*' -or $_.Name -in @('PYTHONUTF8', 'PYTHONIOENCODING')
    } | ForEach-Object { [Environment]::SetEnvironmentVariable($_.Name, $null, 'Process') }
    foreach ($key in $savedEnv.Keys) {
        [Environment]::SetEnvironmentVariable($key, $savedEnv[$key], 'Process')
    }
    $OutputEncoding = $savedOutputEncoding
    [Console]::OutputEncoding = $savedConsoleEncoding
    $report['finishedAt'] = [DateTime]::UtcNow.ToString('o')
    $reportDir = Join-Path $packageRoot 'data/verification'
    try {
        [IO.Directory]::CreateDirectory($reportDir) | Out-Null
        $reportPath = Join-Path $reportDir ("windows-$Stage-" + [Guid]::NewGuid().ToString('N') + '.json')
        [IO.File]::WriteAllText($reportPath, ($report | ConvertTo-Json -Depth 15), (New-Object Text.UTF8Encoding($false)))
        Write-Host "Report: $reportPath"
    } catch {
        Write-Warning ("Could not save report: " + $_.Exception.Message)
    }
}
exit $exitCode
