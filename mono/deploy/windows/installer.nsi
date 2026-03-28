;=============================================================================
; AIPR - AI PR Reviewer Windows Installer
;=============================================================================
;
; This NSIS script creates the Windows installer for AI PR Reviewer.
; It bundles both the C++ Engine and Java Server, with optional service
; installation.
;
; Build:
;   makensis installer.nsi
;
; Output:
;   aipr-${VERSION}-windows-x64-setup.exe
;
;=============================================================================

!include "MUI2.nsh"
!include "FileFunc.nsh"
!include "LogicLib.nsh"
!include "nsDialogs.nsh"
!include "x64.nsh"

;-----------------------------------------------------------------------------
; Configuration
;-----------------------------------------------------------------------------

!define PRODUCT_NAME "AI PR Reviewer"
!define PRODUCT_PUBLISHER "Auralith AI"
!define PRODUCT_WEB_SITE "https://github.com/AuralithAI/RT-AI-PR-Reviewer"
!define PRODUCT_UNINST_KEY "Software\Microsoft\Windows\CurrentVersion\Uninstall\${PRODUCT_NAME}"
!define PRODUCT_UNINST_ROOT_KEY "HKLM"

; Version is set by build process
!ifndef VERSION
  !define VERSION "0.0.0"
!endif

Name "${PRODUCT_NAME} ${VERSION}"
OutFile "aipr-${VERSION}-windows-x64-setup.exe"
InstallDir "$PROGRAMFILES64\AIPR"
InstallDirRegKey HKLM "${PRODUCT_UNINST_KEY}" "InstallLocation"
RequestExecutionLevel admin
ShowInstDetails show
ShowUnInstDetails show

;-----------------------------------------------------------------------------
; Modern UI Configuration
;-----------------------------------------------------------------------------

!define MUI_ABORTWARNING
!define MUI_ICON "..\..\..\docs\images\aipr-icon.ico"
!define MUI_UNICON "..\..\..\docs\images\aipr-icon.ico"
!define MUI_WELCOMEFINISHPAGE_BITMAP "..\..\..\docs\images\installer-banner.bmp"

;-----------------------------------------------------------------------------
; Variables
;-----------------------------------------------------------------------------

Var InstallEngineService
Var InstallServerService
Var Dialog
Var Label
Var CheckboxEngine
Var CheckboxServer

;-----------------------------------------------------------------------------
; Pages
;-----------------------------------------------------------------------------

!insertmacro MUI_PAGE_WELCOME
!insertmacro MUI_PAGE_LICENSE "..\..\..\LICENSE"
!insertmacro MUI_PAGE_DIRECTORY
Page custom ServiceOptionsPage ServiceOptionsPageLeave
!insertmacro MUI_PAGE_INSTFILES
!insertmacro MUI_PAGE_FINISH

!insertmacro MUI_UNPAGE_CONFIRM
!insertmacro MUI_UNPAGE_INSTFILES

!insertmacro MUI_LANGUAGE "English"

;-----------------------------------------------------------------------------
; Service Options Page
;-----------------------------------------------------------------------------

Function ServiceOptionsPage
    !insertmacro MUI_HEADER_TEXT "Service Installation" "Choose which services to install."
    
    nsDialogs::Create 1018
    Pop $Dialog
    
    ${If} $Dialog == error
        Abort
    ${EndIf}
    
    ${NSD_CreateLabel} 0 0 100% 24u "AI PR Reviewer can run as Windows services (recommended for production). You can also run them manually from the command line."
    Pop $Label
    
    ${NSD_CreateCheckbox} 0 40u 100% 12u "Install C++ Engine as Windows service (AIPREngine)"
    Pop $CheckboxEngine
    ${NSD_SetState} $CheckboxEngine ${BST_CHECKED}
    
    ${NSD_CreateCheckbox} 0 60u 100% 12u "Install Java Server as Windows service (AIPRServer)"
    Pop $CheckboxServer
    ${NSD_SetState} $CheckboxServer ${BST_CHECKED}
    
    ${NSD_CreateLabel} 0 90u 100% 48u "Note: Services will be configured to start automatically at boot. You can change this in Windows Services (services.msc). The Engine service must be running before the Server service."
    Pop $Label
    
    nsDialogs::Show
FunctionEnd

Function ServiceOptionsPageLeave
    ${NSD_GetState} $CheckboxEngine $InstallEngineService
    ${NSD_GetState} $CheckboxServer $InstallServerService
FunctionEnd

;-----------------------------------------------------------------------------
; Installer Sections
;-----------------------------------------------------------------------------

Section "Core Files" SecCore
    SectionIn RO ; Required, cannot be deselected
    
    SetOutPath "$INSTDIR"
    
    ; Copy all distribution files
    File /r "..\..\..\build\install\aipr\*.*"
    
    ; Write uninstaller
    WriteUninstaller "$INSTDIR\uninstall.exe"
    
    ; Registry entries
    WriteRegStr HKLM "${PRODUCT_UNINST_KEY}" "DisplayName" "${PRODUCT_NAME}"
    WriteRegStr HKLM "${PRODUCT_UNINST_KEY}" "DisplayVersion" "${VERSION}"
    WriteRegStr HKLM "${PRODUCT_UNINST_KEY}" "Publisher" "${PRODUCT_PUBLISHER}"
    WriteRegStr HKLM "${PRODUCT_UNINST_KEY}" "URLInfoAbout" "${PRODUCT_WEB_SITE}"
    WriteRegStr HKLM "${PRODUCT_UNINST_KEY}" "UninstallString" "$INSTDIR\uninstall.exe"
    WriteRegStr HKLM "${PRODUCT_UNINST_KEY}" "InstallLocation" "$INSTDIR"
    WriteRegDWORD HKLM "${PRODUCT_UNINST_KEY}" "NoModify" 1
    WriteRegDWORD HKLM "${PRODUCT_UNINST_KEY}" "NoRepair" 1
    
    ; Calculate install size
    ${GetSize} "$INSTDIR" "/S=0K" $0 $1 $2
    IntFmt $0 "0x%08X" $0
    WriteRegDWORD HKLM "${PRODUCT_UNINST_KEY}" "EstimatedSize" "$0"
SectionEnd

Section "Add to PATH" SecPath
    ; Add bin directory to system PATH
    EnVar::SetHKLM
    EnVar::AddValue "PATH" "$INSTDIR\bin"
    
    ; Notify shell of environment change
    SendMessage ${HWND_BROADCAST} ${WM_WININICHANGE} 0 "STR:Environment" /TIMEOUT=500
SectionEnd

Section "Start Menu Shortcuts" SecShortcuts
    CreateDirectory "$SMPROGRAMS\${PRODUCT_NAME}"
    
    ; Start services
    CreateShortCut "$SMPROGRAMS\${PRODUCT_NAME}\Start AIPR.lnk" \
        "$INSTDIR\bin\start-aipr.bat" "" \
        "$INSTDIR\bin\aipr-engine.exe" 0
    
    ; Stop services
    CreateShortCut "$SMPROGRAMS\${PRODUCT_NAME}\Stop AIPR.lnk" \
        "$INSTDIR\bin\stop-aipr.bat" "" \
        "$INSTDIR\bin\aipr-engine.exe" 0
    
    ; Web dashboard
    CreateShortCut "$SMPROGRAMS\${PRODUCT_NAME}\Open Web Dashboard.lnk" \
        "http://localhost:8080" "" \
        "$SYSDIR\shell32.dll" 14
    
    ; Config folder
    CreateShortCut "$SMPROGRAMS\${PRODUCT_NAME}\Configuration.lnk" \
        "$INSTDIR\config" "" \
        "$SYSDIR\shell32.dll" 4
    
    ; Uninstaller
    CreateShortCut "$SMPROGRAMS\${PRODUCT_NAME}\Uninstall.lnk" \
        "$INSTDIR\uninstall.exe"
SectionEnd

Section "Install Engine Service" SecEngineService
    ${If} $InstallEngineService == ${BST_CHECKED}
        DetailPrint "Installing AIPREngine service..."
        SetOutPath "$INSTDIR\bin"
        nsExec::ExecToLog '"$INSTDIR\bin\aipr-engine.exe" install'
        Pop $0
        ${If} $0 != 0
            DetailPrint "Warning: Engine service installation returned code $0"
        ${Else}
            DetailPrint "AIPREngine service installed successfully"
        ${EndIf}
    ${EndIf}
SectionEnd

Section "Install Server Service" SecServerService
    ${If} $InstallServerService == ${BST_CHECKED}
        DetailPrint "Installing AIPRServer service..."
        SetOutPath "$INSTDIR\bin"
        nsExec::ExecToLog '"$INSTDIR\bin\aipr-server.exe" install'
        Pop $0
        ${If} $0 != 0
            DetailPrint "Warning: Server service installation returned code $0"
        ${Else}
            DetailPrint "AIPRServer service installed successfully"
        ${EndIf}
    ${EndIf}
SectionEnd

;-----------------------------------------------------------------------------
; Helper Scripts
;-----------------------------------------------------------------------------

Section "-CreateHelperScripts"
    ; Create start-aipr.bat
    FileOpen $0 "$INSTDIR\bin\start-aipr.bat" w
    FileWrite $0 '@echo off$\r$\n'
    FileWrite $0 'echo Starting AIPR services...$\r$\n'
    FileWrite $0 'net start AIPREngine$\r$\n'
    FileWrite $0 'timeout /t 3 /nobreak > nul$\r$\n'
    FileWrite $0 'net start AIPRServer$\r$\n'
    FileWrite $0 'echo.$\r$\n'
    FileWrite $0 'echo AIPR services started.$\r$\n'
    FileWrite $0 'echo Web Dashboard: http://localhost:8080$\r$\n'
    FileWrite $0 'pause$\r$\n'
    FileClose $0
    
    ; Create stop-aipr.bat
    FileOpen $0 "$INSTDIR\bin\stop-aipr.bat" w
    FileWrite $0 '@echo off$\r$\n'
    FileWrite $0 'echo Stopping AIPR services...$\r$\n'
    FileWrite $0 'net stop AIPRServer$\r$\n'
    FileWrite $0 'net stop AIPREngine$\r$\n'
    FileWrite $0 'echo AIPR services stopped.$\r$\n'
    FileWrite $0 'pause$\r$\n'
    FileClose $0
SectionEnd

;-----------------------------------------------------------------------------
; Uninstaller
;-----------------------------------------------------------------------------

Section "Uninstall"
    ; Stop and remove services
    DetailPrint "Stopping services..."
    nsExec::ExecToLog 'net stop AIPRServer'
    nsExec::ExecToLog 'net stop AIPREngine'
    
    DetailPrint "Removing services..."
    nsExec::ExecToLog '"$INSTDIR\bin\aipr-server.exe" uninstall'
    nsExec::ExecToLog '"$INSTDIR\bin\aipr-engine.exe" uninstall'
    
    ; Remove from PATH
    EnVar::SetHKLM
    EnVar::DeleteValue "PATH" "$INSTDIR\bin"
    
    ; Remove Start Menu
    RMDir /r "$SMPROGRAMS\${PRODUCT_NAME}"
    
    ; Remove files
    RMDir /r "$INSTDIR\bin"
    RMDir /r "$INSTDIR\lib"
    RMDir /r "$INSTDIR\config"
    RMDir /r "$INSTDIR\docs"
    RMDir /r "$INSTDIR\certificates"
    Delete "$INSTDIR\uninstall.exe"
    Delete "$INSTDIR\*.txt"
    
    ; Try to remove install directory (will fail if data exists)
    RMDir "$INSTDIR"
    
    ; Remove registry
    DeleteRegKey HKLM "${PRODUCT_UNINST_KEY}"
    
    ; Notify shell of environment change
    SendMessage ${HWND_BROADCAST} ${WM_WININICHANGE} 0 "STR:Environment" /TIMEOUT=500
SectionEnd

;-----------------------------------------------------------------------------
; Section Descriptions
;-----------------------------------------------------------------------------

!insertmacro MUI_FUNCTION_DESCRIPTION_BEGIN
    !insertmacro MUI_DESCRIPTION_TEXT ${SecCore} "Core AIPR files (required)"
    !insertmacro MUI_DESCRIPTION_TEXT ${SecPath} "Add AIPR to system PATH"
    !insertmacro MUI_DESCRIPTION_TEXT ${SecShortcuts} "Create Start Menu shortcuts"
    !insertmacro MUI_DESCRIPTION_TEXT ${SecEngineService} "Install C++ Engine as Windows service"
    !insertmacro MUI_DESCRIPTION_TEXT ${SecServerService} "Install Java Server as Windows service"
!insertmacro MUI_FUNCTION_DESCRIPTION_END
