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
  ; 追加安装目录到系统 PATH。
  ;
  ; 早先这里用 EnVar 插件，但 CI 上 choco 装的 NSIS 不附带该插件，
  ; makensis 直接报 "Plugin not found, cannot call EnVar::AddValue" 并中止打包
  ; （也就是说：NSIS 安装包从来没有真正构建成功过）。
  ; 改为注册表直写 + WM_SETTINGCHANGE 广播，零插件依赖。
  Call KaulaAddToPath
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

  ; 卸载不动系统 PATH：没有 EnVar 插件就无法可靠地做「按分号切分再剔除」，
  ; 而写坏整个 PATH 的代价远大于留下一条指向已删除目录的条目（Windows 会
  ; 直接跳过不存在的路径）。需要彻底清理时请在「系统属性 → 环境变量」里手动删除。

  DeleteRegKey ${PRODUCT_UNINST_ROOT_KEY} "${PRODUCT_UNINST_KEY}"
  DeleteRegKey HKLM "${PRODUCT_DIR_REGKEY}"
  SetAutoClose true
SectionEnd

; ==================== PATH 维护（不依赖 EnVar 插件）====================
;
; 环境块在 HKLM\SYSTEM\CurrentControlSet\Control\Session Manager\Environment，
; Path 是 REG_EXPAND_SZ，故用 WriteRegExpandStr 写入；写完广播 WM_SETTINGCHANGE，
; 否则新开的终端/资源管理器读到的还是旧 PATH。
;
; 早先这里用 EnVar 插件，但 CI 上 choco 装的 NSIS 不附带该插件，makensis 会报
; "Plugin not found, cannot call EnVar::AddValue" 并中止打包 —— 也就是说
; NSIS 安装包从来没真正构建成功过。现在改为注册表直写，零插件依赖。

!define KAULA_ENV_KEY "SYSTEM\CurrentControlSet\Control\Session Manager\Environment"
!define KAULA_HWND_BROADCAST 0xFFFF
!define KAULA_WM_SETTINGCHANGE 0x001A

Function KaulaAddToPath
  ReadRegStr $0 HKLM "${KAULA_ENV_KEY}" "Path"
  StrCmp $0 "" 0 append
  StrCpy $0 "$INSTDIR\bin"
  Goto write
append:
  StrCpy $0 "$0;$INSTDIR\bin"
write:
  WriteRegExpandStr HKLM "${KAULA_ENV_KEY}" "Path" "$0"
  SendMessage ${KAULA_HWND_BROADCAST} ${KAULA_WM_SETTINGCHANGE} 0 "STR:Environment" /TIMEOUT=5000
FunctionEnd

Function .onInit
  ; Check if already installed
  ReadRegStr $0 ${PRODUCT_UNINST_ROOT_KEY} "${PRODUCT_UNINST_KEY}" "UninstallString"
  StrCmp $0 "" done
  MessageBox MB_OK|MB_ICONEXCLAMATION "${PRODUCT_NAME} is already installed. Please uninstall it first."
  abort
done:
FunctionEnd
