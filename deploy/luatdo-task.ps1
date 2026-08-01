# Register the luatdo campaign as a Windows scheduled task, the counterpart of
# the systemd unit and timer. Run it from an elevated PowerShell.
#
# The task runs doctor first and only starts the campaign when a route answers,
# which is the same guard the systemd unit uses.

[CmdletBinding()]
param(
    [string]$Exe = "C:\Program Files\luatdo\luatdo.exe",
    [string]$Data = "C:\ProgramData\luatdo",
    [string]$Routes = "C:\ProgramData\luatdo\routes.json",
    [string]$TaskName = "luatdo campaign",
    [int]$IntervalHours = 1
)

$ErrorActionPreference = "Stop"

if (-not (Test-Path $Exe)) {
    throw "luatdo.exe not found at $Exe, install the release binary first"
}
New-Item -ItemType Directory -Force -Path $Data | Out-Null

# cmd /c chains the guard and the campaign the way ExecStartPre does: the
# campaign only runs if doctor exited zero.
$command = '"{0}" doctor && "{0}" run --parallel auto' -f $Exe
$action = New-ScheduledTaskAction -Execute "cmd.exe" -Argument "/c $command" -WorkingDirectory $Data

$trigger = New-ScheduledTaskTrigger -Once -At (Get-Date) `
    -RepetitionInterval (New-TimeSpan -Hours $IntervalHours)

$settings = New-ScheduledTaskSettingsSet `
    -StartWhenAvailable `
    -DontStopOnIdleEnd `
    -MultipleInstances IgnoreNew `
    -ExecutionTimeLimit (New-TimeSpan -Hours 12)

[Environment]::SetEnvironmentVariable("LUATDO_DATA", $Data, "Machine")
[Environment]::SetEnvironmentVariable("LUATDO_ROUTES", $Routes, "Machine")

Register-ScheduledTask -TaskName $TaskName -Action $action -Trigger $trigger `
    -Settings $settings -RunLevel Highest -Force | Out-Null

Write-Host "registered '$TaskName', data $Data, routes $Routes"
Write-Host "start it now with: Start-ScheduledTask -TaskName '$TaskName'"
Write-Host "stop a run with:   Stop-ScheduledTask -TaskName '$TaskName'"
