# =============================================================================
# AI PR Reviewer - Infrastructure Setup Script (Windows PowerShell)
# =============================================================================
# This script sets up the development/CI environment by downloading and
# configuring the required toolchains (JRE, Gradle, CMake).
#
# Usage:
#   .\scripts\setup.ps1 [options]
#
# Options:
#   -CI              CI mode (non-interactive, stricter)
#   -SkipJRE         Skip JRE download (use system Java)
#   -SkipGradle      Skip Gradle download (use system Gradle)
#   -SkipCMake       Skip CMake setup
#   -Clean           Clean existing downloads before setup
#   -Help            Show this help message
# =============================================================================

[CmdletBinding()]
param(
    [switch]$CI,
    [switch]$SkipJRE,
    [switch]$SkipGradle,
    [switch]$SkipCMake,
    [switch]$Clean,
    [switch]$Help
)

$ErrorActionPreference = "Stop"

# Configuration
$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$RootDir = Split-Path -Parent $ScriptDir
$InfraDir = Join-Path $RootDir "infrastructure_src"

# Versions
$JREVersion = "21.0.2+13"
$JREMajorVersion = "21"
$GradleVersion = "8.5"
$CMakeVersion = "3.28.1"

# Logging functions
function Log-Info { param($Message) Write-Host "[INFO] $Message" -ForegroundColor Cyan }
function Log-Warn { param($Message) Write-Host "[WARN] $Message" -ForegroundColor Yellow }
function Log-Error { param($Message) Write-Host "[ERROR] $Message" -ForegroundColor Red }
function Log-Success { param($Message) Write-Host "[SUCCESS] $Message" -ForegroundColor Green }

# Detect architecture
function Get-Architecture {
    if ([Environment]::Is64BitOperatingSystem) {
        return "x64"
    }
    return "x86"
}

# Download file with progress
function Download-File {
    param(
        [string]$Url,
        [string]$Output
    )
    
    Log-Info "Downloading: $Url"
    
    $ProgressPreference = 'SilentlyContinue'  # Faster downloads
    try {
        Invoke-WebRequest -Uri $Url -OutFile $Output -UseBasicParsing
        $ProgressPreference = 'Continue'
        return $true
    }
    catch {
        $ProgressPreference = 'Continue'
        Log-Error "Download failed: $_"
        return $false
    }
}

# Extract archive
function Extract-Archive {
    param(
        [string]$Archive,
        [string]$Destination
    )
    
    Log-Info "Extracting: $Archive"
    
    if (-not (Test-Path $Destination)) {
        New-Item -ItemType Directory -Path $Destination -Force | Out-Null
    }
    
    if ($Archive -match '\.zip$') {
        # Extract to temp, then move contents (to strip top-level folder)
        $TempDir = Join-Path $env:TEMP ([System.Guid]::NewGuid().ToString())
        Expand-Archive -Path $Archive -DestinationPath $TempDir -Force
        
        $TopDir = Get-ChildItem -Path $TempDir -Directory | Select-Object -First 1
        if ($TopDir) {
            Get-ChildItem -Path $TopDir.FullName | Move-Item -Destination $Destination -Force
        }
        else {
            Get-ChildItem -Path $TempDir | Move-Item -Destination $Destination -Force
        }
        
        Remove-Item -Path $TempDir -Recurse -Force
    }
    elseif ($Archive -match '\.(tar\.gz|tgz)$') {
        # Use tar if available (Windows 10+)
        if (Get-Command tar -ErrorAction SilentlyContinue) {
            tar -xzf $Archive -C $Destination --strip-components=1
        }
        else {
            Log-Error "tar is not available. Please extract manually or install tar."
            return $false
        }
    }
    else {
        Log-Error "Unknown archive format: $Archive"
        return $false
    }
    
    return $true
}

# Setup JRE
function Setup-JRE {
    $JREDir = Join-Path $InfraDir "jre"
    
    if (Test-Path (Join-Path $JREDir "bin\java.exe")) {
        Log-Info "JRE already exists at $JREDir"
        return $true
    }
    
    Log-Info "Setting up JRE $JREVersion for Windows..."
    
    $Arch = Get-Architecture
    $JREVersionUnderscore = $JREVersion -replace '\+', '_'
    $JREUrl = "https://github.com/adoptium/temurin$JREMajorVersion-binaries/releases/download/jdk-$JREVersion/OpenJDK${JREMajorVersion}U-jre_${Arch}_windows_hotspot_$JREVersionUnderscore.zip"
    
    $DownloadDir = Join-Path $InfraDir "downloads"
    if (-not (Test-Path $DownloadDir)) {
        New-Item -ItemType Directory -Path $DownloadDir -Force | Out-Null
    }
    
    $ArchiveFile = Join-Path $DownloadDir "jre.zip"
    
    if (-not (Download-File -Url $JREUrl -Output $ArchiveFile)) {
        # Try Amazon Corretto as fallback
        Log-Warn "Failed to download Adoptium JRE, trying Amazon Corretto..."
        $JREUrl = "https://corretto.aws/downloads/latest/amazon-corretto-$JREMajorVersion-$Arch-windows-jdk.zip"
        if (-not (Download-File -Url $JREUrl -Output $ArchiveFile)) {
            return $false
        }
    }
    
    if (-not (Test-Path $JREDir)) {
        New-Item -ItemType Directory -Path $JREDir -Force | Out-Null
    }
    
    if (-not (Extract-Archive -Archive $ArchiveFile -Destination $JREDir)) {
        return $false
    }
    
    # Verify
    $JavaExe = Join-Path $JREDir "bin\java.exe"
    if (Test-Path $JavaExe) {
        $Version = & $JavaExe -version 2>&1 | Select-Object -First 1
        Log-Success "JRE installed: $Version"
        return $true
    }
    else {
        Log-Error "JRE installation failed"
        return $false
    }
}

# Setup Gradle
function Setup-Gradle {
    $GradleDir = Join-Path $InfraDir "gradle"
    
    if (Test-Path (Join-Path $GradleDir "bin\gradle.bat")) {
        Log-Info "Gradle already exists at $GradleDir"
        return $true
    }
    
    Log-Info "Setting up Gradle $GradleVersion..."
    
    $GradleUrl = "https://services.gradle.org/distributions/gradle-$GradleVersion-bin.zip"
    
    $DownloadDir = Join-Path $InfraDir "downloads"
    if (-not (Test-Path $DownloadDir)) {
        New-Item -ItemType Directory -Path $DownloadDir -Force | Out-Null
    }
    
    $ArchiveFile = Join-Path $DownloadDir "gradle.zip"
    
    if (-not (Download-File -Url $GradleUrl -Output $ArchiveFile)) {
        return $false
    }
    
    if (-not (Test-Path $GradleDir)) {
        New-Item -ItemType Directory -Path $GradleDir -Force | Out-Null
    }
    
    if (-not (Extract-Archive -Archive $ArchiveFile -Destination $GradleDir)) {
        return $false
    }
    
    # Verify
    $GradleBat = Join-Path $GradleDir "bin\gradle.bat"
    if (Test-Path $GradleBat) {
        Log-Success "Gradle installed: $GradleVersion"
        return $true
    }
    else {
        Log-Error "Gradle installation failed"
        return $false
    }
}

# Setup CMake
function Setup-CMake {
    # Check if CMake is already available on system
    if (Get-Command cmake -ErrorAction SilentlyContinue) {
        $SystemVersion = (cmake --version | Select-Object -First 1) -replace 'cmake version ', ''
        Log-Info "System CMake found: $SystemVersion"
        return $true
    }
    
    $CMakeDir = Join-Path $InfraDir "cmake"
    
    if (Test-Path (Join-Path $CMakeDir "bin\cmake.exe")) {
        Log-Info "CMake already exists at $CMakeDir"
        return $true
    }
    
    Log-Info "Setting up CMake $CMakeVersion..."
    
    $Arch = Get-Architecture
    $CMakeUrl = "https://github.com/Kitware/CMake/releases/download/v$CMakeVersion/cmake-$CMakeVersion-windows-$Arch.zip"
    
    $DownloadDir = Join-Path $InfraDir "downloads"
    if (-not (Test-Path $DownloadDir)) {
        New-Item -ItemType Directory -Path $DownloadDir -Force | Out-Null
    }
    
    $ArchiveFile = Join-Path $DownloadDir "cmake.zip"
    
    if (-not (Download-File -Url $CMakeUrl -Output $ArchiveFile)) {
        Log-Warn "Failed to download CMake. Please install manually."
        return $true  # Non-fatal
    }
    
    if (-not (Test-Path $CMakeDir)) {
        New-Item -ItemType Directory -Path $CMakeDir -Force | Out-Null
    }
    
    if (-not (Extract-Archive -Archive $ArchiveFile -Destination $CMakeDir)) {
        return $true  # Non-fatal
    }
    
    # Verify
    $CMakeExe = Join-Path $CMakeDir "bin\cmake.exe"
    if (Test-Path $CMakeExe) {
        Log-Success "CMake installed: $CMakeVersion"
        return $true
    }
    else {
        Log-Warn "CMake installation may have issues"
        return $true  # Non-fatal
    }
}

# Generate environment file
function Generate-EnvFile {
    $EnvFile = Join-Path $InfraDir "env.ps1"
    
    Log-Info "Generating environment file: $EnvFile"
    
    $EnvContent = @'
# AI PR Reviewer - Environment Configuration (PowerShell)
# Dot-source this file to set up your environment:
#   . infrastructure_src\env.ps1

$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$RootDir = Split-Path -Parent $ScriptDir
$InfraDir = $ScriptDir

# Java
$JREDir = Join-Path $InfraDir "jre"
if (Test-Path (Join-Path $JREDir "bin\java.exe")) {
    $env:JAVA_HOME = $JREDir
    $env:PATH = "$JREDir\bin;$env:PATH"
}

# Gradle
$GradleDir = Join-Path $InfraDir "gradle"
if (Test-Path (Join-Path $GradleDir "bin\gradle.bat")) {
    $env:GRADLE_HOME = $GradleDir
    $env:PATH = "$GradleDir\bin;$env:PATH"
}

# CMake
$CMakeDir = Join-Path $InfraDir "cmake"
if (Test-Path (Join-Path $CMakeDir "bin\cmake.exe")) {
    $env:CMAKE_HOME = $CMakeDir
    $env:PATH = "$CMakeDir\bin;$env:PATH"
}

# AI PR Reviewer
$env:AIPR_HOME = $RootDir
$VersionFile = Join-Path $RootDir "VERSION"
if (Test-Path $VersionFile) {
    $env:AIPR_VERSION = (Get-Content $VersionFile -Raw).Trim()
} else {
    $env:AIPR_VERSION = "dev"
}

Write-Host "Environment configured for AI PR Reviewer"
Write-Host "  JAVA_HOME:   $($env:JAVA_HOME ?? 'system')"
Write-Host "  GRADLE_HOME: $($env:GRADLE_HOME ?? 'system')"
Write-Host "  CMAKE_HOME:  $($env:CMAKE_HOME ?? 'system')"
Write-Host "  AIPR_HOME:   $env:AIPR_HOME"
'@
    
    Set-Content -Path $EnvFile -Value $EnvContent -Encoding UTF8
    Log-Success "Environment file generated"
}

# Generate batch file wrapper
function Generate-EnvBatch {
    $EnvBat = Join-Path $InfraDir "env.bat"
    
    $BatContent = @"
@echo off
REM AI PR Reviewer - Environment Configuration (Batch)
REM Run this file to set up your environment:
REM   call infrastructure_src\env.bat

set "INFRA_DIR=%~dp0"
set "ROOT_DIR=%INFRA_DIR%.."

REM Java
if exist "%INFRA_DIR%jre\bin\java.exe" (
    set "JAVA_HOME=%INFRA_DIR%jre"
    set "PATH=%JAVA_HOME%\bin;%PATH%"
)

REM Gradle
if exist "%INFRA_DIR%gradle\bin\gradle.bat" (
    set "GRADLE_HOME=%INFRA_DIR%gradle"
    set "PATH=%GRADLE_HOME%\bin;%PATH%"
)

REM CMake
if exist "%INFRA_DIR%cmake\bin\cmake.exe" (
    set "CMAKE_HOME=%INFRA_DIR%cmake"
    set "PATH=%CMAKE_HOME%\bin;%PATH%"
)

REM AI PR Reviewer
set "AIPR_HOME=%ROOT_DIR%"

echo Environment configured for AI PR Reviewer
"@
    
    Set-Content -Path $EnvBat -Value $BatContent -Encoding ASCII
}

# Clean downloads
function Clean-Downloads {
    Log-Info "Cleaning downloads..."
    
    $DownloadDir = Join-Path $InfraDir "downloads"
    if (Test-Path $DownloadDir) { Remove-Item -Path $DownloadDir -Recurse -Force }
    
    $JREDir = Join-Path $InfraDir "jre"
    if (Test-Path $JREDir) { Remove-Item -Path $JREDir -Recurse -Force }
    
    $GradleDir = Join-Path $InfraDir "gradle"
    if (Test-Path $GradleDir) { Remove-Item -Path $GradleDir -Recurse -Force }
    
    $CMakeDir = Join-Path $InfraDir "cmake"
    if (Test-Path $CMakeDir) { Remove-Item -Path $CMakeDir -Recurse -Force }
    
    Log-Success "Cleaned infrastructure downloads"
}

# Print help
function Show-Help {
    Get-Help $MyInvocation.MyCommand.Path -Detailed
}

# Main
function Main {
    if ($Help) {
        Show-Help
        return
    }
    
    Write-Host "=============================================="
    Write-Host "AI PR Reviewer - Infrastructure Setup"
    Write-Host "=============================================="
    Write-Host ""
    
    $Arch = Get-Architecture
    Log-Info "Detected platform: windows-$Arch"
    
    # Clean if requested
    if ($Clean) {
        Clean-Downloads
    }
    
    # Create infrastructure directory
    if (-not (Test-Path $InfraDir)) {
        New-Item -ItemType Directory -Path $InfraDir -Force | Out-Null
    }
    
    # Setup components
    if (-not $SkipJRE) {
        if (-not (Setup-JRE)) {
            if ($CI) {
                Log-Error "JRE setup failed in CI mode"
                exit 1
            }
            else {
                Log-Warn "JRE setup failed, will use system Java"
            }
        }
    }
    
    if (-not $SkipGradle) {
        if (-not (Setup-Gradle)) {
            if ($CI) {
                Log-Error "Gradle setup failed in CI mode"
                exit 1
            }
            else {
                Log-Warn "Gradle setup failed, will use system Gradle"
            }
        }
    }
    
    if (-not $SkipCMake) {
        Setup-CMake | Out-Null
    }
    
    # Generate environment files
    Generate-EnvFile
    Generate-EnvBatch
    
    # Cleanup downloads to save space in CI
    if ($CI) {
        Log-Info "Cleaning up download archives..."
        $DownloadDir = Join-Path $InfraDir "downloads"
        if (Test-Path $DownloadDir) {
            Remove-Item -Path $DownloadDir -Recurse -Force
        }
    }
    
    Write-Host ""
    Write-Host "=============================================="
    Log-Success "Setup complete!"
    Write-Host ""
    Write-Host "To configure your environment, run:"
    Write-Host "  . infrastructure_src\env.ps1"
    Write-Host "=============================================="
}

Main
