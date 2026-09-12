@echo off
rem release: 前端 + core 全量构建 -> electron-builder 出 NSIS 安装包 + 绿色 zip
rem 输出 desktop/release/（安装包内嵌 ezharness-core.exe 与图标）
setlocal
cd /d "%~dp0.."

cd frontend
call npm run build
if errorlevel 1 goto :err
cd ..
powershell -NoProfile -Command "Remove-Item -Recurse -Force core/dist -ErrorAction SilentlyContinue; Copy-Item -Recurse frontend/dist core/dist"
cd core
go build -o ..\ezharness-core.exe .
if errorlevel 1 goto :err
cd ..

cd desktop
call npx electron-builder --win
if errorlevel 1 goto :err
echo.
echo release done: desktop\release\
goto :eof

:err
echo release.bat failed
exit /b 1
