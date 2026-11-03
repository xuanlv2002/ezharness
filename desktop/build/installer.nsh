# Windows NSIS 安装器自定义脚本。
#
# 升级救援：旧版安装版（≤ v1.1.2）把 ezharness.json 与 data/ 放在安装目录
# resources/ 下，NSIS 升级会先跑旧版卸载器把整个安装目录删掉。customInit
# 在卸载旧版之前执行，这里把仍存在的旧配置与数据预拷到新版本使用的用户
# 数据目录（%APPDATA%\ezharness，与 Electron userData、app.setName 一致）；
# 只补缺——目标已有 data/ 就整体跳过，json 也只拷到空位，不覆盖已有内容。
!include LogicLib.nsh

!macro customInit
  ${IfNot} ${FileExists} "$APPDATA\ezharness\data\*.*"
    ${If} ${FileExists} "$INSTDIR\resources\data\*.*"
      CreateDirectory "$APPDATA\ezharness"
      nsExec::ExecToLog 'xcopy /E /I /Y /Q "$INSTDIR\resources\data" "$APPDATA\ezharness\data"'
    ${EndIf}
    ${If} ${FileExists} "$INSTDIR\resources\ezharness.json"
      ${AndIfNot} ${FileExists} "$APPDATA\ezharness\ezharness.json"
      CreateDirectory "$APPDATA\ezharness"
      CopyFiles /SILENT "$INSTDIR\resources\ezharness.json" "$APPDATA\ezharness"
    ${EndIf}
  ${EndIf}
!macroend
