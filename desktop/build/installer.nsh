# Windows NSIS 安装器自定义脚本。
#
# 1) 升级救援：旧版安装版（≤ v1.1.2）把 ezharness.json 与 data/ 放在安装目录
#    resources/ 下，NSIS 升级会先跑旧版卸载器把整个安装目录删掉。customInit
#    在卸载旧版之前执行，这里只把仍存在的旧 data/ 预拷到新版本使用的用户
#    数据目录（%APPDATA%\ezharness，与 Electron userData、app.setName 一致）；
#    目标已有 data/ 就整体跳过。ezharness.json 不迁移，由 core 首次启动按默认重建。
# 2) PATH：安装目录页之后加一张勾选页（默认勾选），把安装目录追加进 HKLM
#    系统 PATH——perMachine 安装已提升，所有用户的任意终端都能直达 ezharness。
#    判重用标记位：同目录升级只写一次；换目录安装才再追加（旧项悬空无害，
#    不做卸载清理）。广播 WM_SETTINGCHANGE 让新开的终端立即可见。
# 3) 常驻 core 会锁住 ezharness-core.exe：安装/卸载前先停掉，否则文件覆盖
#    或删除失败（数据已逐轮落盘，停止不丢配置；下次启动自动重新拉起）。
#
# 本文件含中文 UI 文案，必须以 UTF-8 BOM 保存，否则 makensis 按 ANSI 解码会乱码。
!include LogicLib.nsh
!include WinMessages.nsh
!include nsDialogs.nsh

# electron-builder 拿 executableName（ezharness-desktop）当安装目录名；应用目录
# 该是产品名 ezharness。只覆盖目录相关的 APP_FILENAME——exe 名走
# PRODUCT_FILENAME，快捷方式走 SHORTCUT_NAME，都不受影响。
!undef APP_FILENAME
!define APP_FILENAME "ezharness"

!macro customInit
  nsExec::ExecToLog 'taskkill /IM ezharness-core.exe /F /T'
  ${IfNot} ${FileExists} "$APPDATA\ezharness\data\*.*"
    ${If} ${FileExists} "$INSTDIR\resources\data\*.*"
      CreateDirectory "$APPDATA\ezharness"
      nsExec::ExecToLog 'xcopy /E /I /Y /Q "$INSTDIR\resources\data" "$APPDATA\ezharness\data"'
    ${EndIf}
  ${EndIf}
!macroend

# 安装目录页之后、开始安装之前：是否把安装目录并入系统 PATH。
# 页面函数定义在此宏内——插入点在 MUI2 之后，MUI_HEADER_TEXT 才可用。
!macro customPageAfterChangeDir
  Var AddPathCheckbox
  Var AddPathState

  Page custom AddPathPageCreate AddPathPageLeave

  Function AddPathPageCreate
    !insertmacro MUI_HEADER_TEXT "环境变量" "勾选后可在任意终端直接运行 ezharness 命令。"
    nsDialogs::Create 1018
    Pop $0
    ${If} $0 == error
      Abort
    ${EndIf}
    ${NSD_CreateCheckbox} 0 0 100% 12u "把 ezharness 加入系统 PATH 环境变量（推荐）"
    Pop $AddPathCheckbox
    ${If} $AddPathState != "0"
      ${NSD_Check} $AddPathCheckbox
    ${EndIf}
    nsDialogs::Show
  FunctionEnd

  Function AddPathPageLeave
    ${NSD_GetState} $AddPathCheckbox $AddPathState
  FunctionEnd
!macroend

!macro customInstall
  ${If} $AddPathState != "0"
    ReadRegStr $0 HKLM "Software\ezharness" "PathDir"
    ${If} $0 != "$INSTDIR"
      ReadRegStr $1 HKLM "SYSTEM\CurrentControlSet\Control\Session Manager\Environment" "Path"
      ${If} $1 == ""
        WriteRegExpandStr HKLM "SYSTEM\CurrentControlSet\Control\Session Manager\Environment" "Path" "$INSTDIR"
      ${Else}
        WriteRegExpandStr HKLM "SYSTEM\CurrentControlSet\Control\Session Manager\Environment" "Path" "$1;$INSTDIR"
      ${EndIf}
      WriteRegStr HKLM "Software\ezharness" "PathDir" "$INSTDIR"
      SendMessage ${HWND_BROADCAST} ${WM_SETTINGCHANGE} 0 "STR:Environment" /TIMEOUT=5000
      DetailPrint "PATH += $INSTDIR"
    ${EndIf}
  ${EndIf}
!macroend

!macro customUnInstall
  nsExec::ExecToLog 'taskkill /IM ezharness-core.exe /F /T'
!macroend
