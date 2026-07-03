param(
    [string]$RemoteHost = "strike",
    [string]$RemotePath = "/home/evil/bsky-spoiler-telegram-bot",
    [string]$BinaryName = "bsky-spoiler-telegram-bot",
    [string]$KeyPath = "$env:USERPROFILE\.ssh\keys\strike.ppk.openssh"
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

# # Start a temporary ssh-agent and load the key once
# Write-Host "Starting SSH agent..." -ForegroundColor Cyan
# ssh-agent | Invoke-Expression
# ssh-add $KeyPath

# if ($LASTEXITCODE -ne 0) {
#     Write-Error "Failed to add SSH key"
#     exit 1
# }

try {
    # Build for Linux amd64
    Write-Host "Building for Linux..." -ForegroundColor Cyan
    $env:GOOS = "linux"
    $env:GOARCH = "amd64"
    $env:CGO_ENABLED = "0"

    go build -o $BinaryName .

    if ($LASTEXITCODE -ne 0) {
        Write-Error "Build failed"
        exit 1
    }

    Write-Host "Build succeeded: $BinaryName" -ForegroundColor Green

    # Ensure remote directory exists and back up existing binary
    Write-Host "Preparing remote directory..." -ForegroundColor Cyan
    ssh $RemoteHost "mkdir -p ${RemotePath} && [ -f ${RemotePath}/${BinaryName} ] && mv ${RemotePath}/${BinaryName} ${RemotePath}/${BinaryName}.old || true"

    if ($LASTEXITCODE -ne 0) {
        Write-Error "Failed to prepare remote directory"
        exit 1
    }

    # Upload binary via SCP
    Write-Host "Uploading to ${RemoteHost}:${RemotePath} ..." -ForegroundColor Cyan
    scp $BinaryName "${RemoteHost}:${RemotePath}/${BinaryName}"

    if ($LASTEXITCODE -ne 0) {
        Write-Error "SCP upload failed"
        exit 1
    }

    Write-Host "Upload succeeded" -ForegroundColor Green

    ssh $RemoteHost "chmod +x ${RemotePath}/${BinaryName}"

    if ($LASTEXITCODE -ne 0) {
        Write-Error "chmod failed"
        exit 1
    }

    # Restart the service over SSH
    Write-Host "Restarting bsky-spoiler-telegram-bot.service ..." -ForegroundColor Cyan
    ssh -t $RemoteHost "sudo systemctl restart bsky-spoiler-telegram-bot.service"

    if ($LASTEXITCODE -ne 0) {
        Write-Error "Service restart failed"
        exit 1
    }

    Write-Host "Service restarted successfully" -ForegroundColor Green
} finally {
    # # Clean up local Linux binary and stop the SSH agent
    # if (Test-Path $BinaryName) { Remove-Item $BinaryName }
    # ssh-agent -k | Out-Null
}
