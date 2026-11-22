# Windows NSIS 安装器自定义脚本。
#
# 1) 升级救援：旧版安装版（≤ v1.1.2）把 ezharness.json 与 data/ 放在安装目录
#    resources/ 下，NSIS 升级会先跑旧版卸载器把整个安装目录删掉。customInit
#    在卸载旧版之前执行，这里只把仍存在的旧 data/ 预拷到新版本使用的用户
#    数据目录（%APPDATA%\ezharness，与 Electron userData、app.setName 一致）；
#    目标已有 data/ 就整体跳过。ezharness.json 不迁移，由 core 首次启动按默认重建。
# 2) PATH：安装目录追加进 HKCU Environment\Path（三 exe 并排，ezharness 命令
#    直达）。判重用标记位：同目录升级只写一次；换目录安装才再追加（旧项
#    悬空无害，不做卸载清理）。广播 WM_SETTINGCHANGE 让新开的终端立即可见。
# 3) 常驻 core 会锁住 ezharness-core.exe：安装/卸载前先停掉，否则文件覆盖
#    或删除失败（数据已逐轮落盘，停止不丢配置；下次启动自动重新拉起）。
!include LogicLib.nsh
!include WinMessages.nsh

!macro customInit
  nsExec::ExecToLog 'taskkill /IM ezharness-core.exe /F /T'
  ${IfNot} ${FileExists} "$APPDATA\ezharness\data\*.*"
    ${If} ${FileExists} "$INSTDIR\resources\data\*.*"
      CreateDirectory "$APPDATA\ezharness"
      nsExec::ExecToLog 'xcopy /E /I /Y /Q "$INSTDIR\resources\data" "$APPDATA\ezharness\data"'
    ${EndIf}
  ${EndIf}
!macroend

!macro customUnInstall
  nsExec::ExecToLog 'taskkill /IM ezharness-core.exe /F /T'
!macroend

!macro customInstall
  ReadRegStr $0 HKCU "Software\ezharness" "PathDir"
  ${If} $0 != "$INSTDIR"
    ReadRegStr $1 HKCU "Environment" "Path"
    ${If} $1 == ""
      WriteRegExpandStr HKCU "Environment" "Path" "$INSTDIR"
    ${Else}
      WriteRegExpandStr HKCU "Environment" "Path" "$1;$INSTDIR"
    ${EndIf}
    WriteRegStr HKCU "Software\ezharness" "PathDir" "$INSTDIR"
    SendMessage ${HWND_BROADCAST} ${WM_SETTINGCHANGE} 0 "STR:Environment" /TIMEOUT=5000
    DetailPrint "PATH += $INSTDIR"
  ${EndIf}
!macroend
