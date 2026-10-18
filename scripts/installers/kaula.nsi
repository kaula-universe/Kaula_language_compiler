; Kaula Language Installer for Windows
; NSIS installer script

!define PRODUCT_NAME "Kaula"
!define PRODUCT_PUBLISHER "Kaula Universe"
!define PRODUCT_WEB_SITE "https://gitee.com/zhe116/kaula"
!define PRODUCT_DIR_REGKEY "Software\Microsoft\Windows\CurrentVersion\App Paths\kaulac.exe"
!define PRODUCT_UNINST_KEY "Software\Microsoft\Windows\CurrentVersion\Uninstall\${PRODUCT_NAME}"
!define PRODUCT_UNINST_ROOT_KEY "HKLM"

; Version from build
;
; 版本号由命令行传入（release.yml: makensis /DPRODUCT_VERSION=... /D..._MAJOR=...）。
; 这里必须用 !ifndef 兜底 —— 不加保护时「命令行已定义 + 脚本内再 !define」会让
; makensis 直接报 `!define: "PRODUCT_VERSION" already defined!` 并中止打包。
!ifndef PRODUCT_VERSION
    !define PRODUCT_VERSION "0.0.0"
!endif
!ifndef PRODUCT_VERSION_MAJOR
    !define PRODUCT_VERSION_MAJOR "0"
!endif
!ifndef PRODUCT_VERSION_MINOR
    !define PRODUCT_VERSION_MINOR "0"
!endif
!ifndef PRODUCT_VERSION_PATCH
    !define PRODUCT_VERSION_PATCH "0"
!endif

SetCompressor /SOLID lzma

; MUI 1.6 compatible ------
!include "MUI.nsh"

; MUI Settings
!define MUI_ABORTWARNING
!define MUI_ICON "${NSISDIR}\Contrib\Graphics\Icons\modern-install.ico"
!define MUI_UNICON "${NSISDIR}\Contrib\Graphics\Icons\modern-uninstall.ico"

; Welcome page
!insertmacro MUI_PAGE_WELCOME
; License page
!insertmacro MUI_PAGE_LICENSE "..\..\LICENSE"
; Directory page
!insertmacro MUI_PAGE_DIRECTORY
; Inst files page
!insertmacro MUI_PAGE_INSTFILES
; Finish page
!define MUI_FINISHPAGE_RUN "$INSTDIR\bin\kaulac.exe"
!insertmacro MUI_PAGE_FINISH

; Uninstaller pages
!insertmacro MUI_UNPAGE_INSTFILES

; Language files
!insertmacro MUI_LANGUAGE "English"
!insertmacro MUI_LANGUAGE "SimpChinese"

; Reserve files
!insertmacro MUI_RESERVEFILE_INSTALLOPTIONS

; MUI end ------

Name "${PRODUCT_NAME} ${PRODUCT_VERSION}"
OutFile "..\..\release\kaula-${PRODUCT_VERSION}-windows-x64.exe"
InstallDir "$PROGRAMFILES64\Kaula"
InstallDirRegKey HKLM "${PRODUCT_DIR_REGKEY}" ""
ShowInstDetails show
ShowUnInstDetails show

Section "MainSection" SEC01
  SetOutPath "$INSTDIR"
  SetOverwrite ifnewer
  File /r "..\..\build\bin"
  File /r "..\..\build\lib"
  File /r "..\..\build\include"
  File "..\..\README.md"
  File "..\..\LICENSE"
  File "..\..\toolkit_build.py"
  File /r "..\..\std"
  File /r "..\..\src"
  File /r "..\..\scripts"
  File /r "..\..\docs"
  File /r "..\..\tools"
SectionEnd

Section -AddToPath
  ; Add to PATH
  EnVar::AddValue "PATH" "$INSTDIR\bin"
SectionEnd

Section -Post
  WriteUninstaller "$INSTDIR\uninstall.exe"
  WriteRegStr HKLM "${PRODUCT_DIR_REGKEY}" "" "$INSTDIR\bin\kaulac.exe"
  WriteRegStr ${PRODUCT_UNINST_ROOT_KEY} "${PRODUCT_UNINST_KEY}" "DisplayName" "$(^Name)"
  WriteRegStr ${PRODUCT_UNINST_ROOT_KEY} "${PRODUCT_UNINST_KEY}" "UninstallString" "$INSTDIR\uninstall.exe"
  WriteRegStr ${PRODUCT_UNINST_ROOT_KEY} "${PRODUCT_UNINST_KEY}" "DisplayIcon" "$INSTDIR\bin\kaulac.exe"
  WriteRegStr ${PRODUCT_UNINST_ROOT_KEY} "${PRODUCT_UNINST_KEY}" "DisplayVersion" "${PRODUCT_VERSION}"
  WriteRegStr ${PRODUCT_UNINST_ROOT_KEY} "${PRODUCT_UNINST_KEY}" "URLInfoAbout" "${PRODUCT_WEB_SITE}"
  WriteRegStr ${PRODUCT_UNINST_ROOT_KEY} "${PRODUCT_UNINST_KEY}" "Publisher" "${PRODUCT_PUBLISHER}"
SectionEnd

Section Uninstall
  Delete "$INSTDIR\uninstall.exe"
  RMDir /r "$INSTDIR\bin"
  RMDir /r "$INSTDIR\lib"
  RMDir /r "$INSTDIR\include"
  RMDir /r "$INSTDIR\std"
  RMDir /r "$INSTDIR\src"
  RMDir /r "$INSTDIR\scripts"
  RMDir /r "$INSTDIR\docs"
  RMDir /r "$INSTDIR\tools"
  Delete "$INSTDIR\README.md"
  Delete "$INSTDIR\LICENSE"
  Delete "$INSTDIR\toolkit_build.py"
  RMDir "$INSTDIR"

  EnVar::DeleteValue "PATH" "$INSTDIR\bin"

  DeleteRegKey ${PRODUCT_UNINST_ROOT_KEY} "${PRODUCT_UNINST_KEY}"
  DeleteRegKey HKLM "${PRODUCT_DIR_REGKEY}"
  SetAutoClose true
SectionEnd

Function .onInit
  ; Check if already installed
  ReadRegStr $0 ${PRODUCT_UNINST_ROOT_KEY} "${PRODUCT_UNINST_KEY}" "UninstallString"
  StrCmp $0 "" done
  MessageBox MB_OK|MB_ICONEXCLAMATION "${PRODUCT_NAME} is already installed. Please uninstall it first."
  abort
done:
FunctionEnd
