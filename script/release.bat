@echo off
rem release: versioned full build (version source: script/version.yaml)
rem chain: frontend build -> copy dist to core/web/dist -> go build (bin/) -> electron-builder
rem output:
rem   release\v<version>\ezharness-v<version>-setup.exe   NSIS installer
rem   release\v<version>\ezharness-v<version>.exe         portable exe
setlocal EnableExtensions
cd /d "%~dp0.."

for /f "tokens=2 delims=: " %%v in ('findstr /b /c:"version:" script\version.yaml') do set VERSION=%%v
if not defined VERSION exit /b 1

rem sync version: frontend gets v-prefixed string (footer: ezharness-v<version>), desktop gets bare semver
set SRCVER=%VERSION%
powershell -NoProfile -Command "$v = $env:SRCVER; $f = 'frontend\package.json'; $j = Get-Content $f -Raw -Encoding UTF8; $j = $j -replace '\"version\": \"[^\"]*\"', ('\"version\": \"v' + $v + '\"'); [IO.File]::WriteAllText($f, $j); $d = 'desktop\package.json'; $j = Get-Content $d -Raw -Encoding UTF8; $j = $j -replace '\"version\": \"[^\"]*\"', ('\"version\": \"' + $v + '\"'); [IO.File]::WriteAllText($d, $j)"
if errorlevel 1 exit /b 1

echo [release] building frontend...
cd frontend
call npm run build
if errorlevel 1 exit /b 1
cd ..

powershell -NoProfile -Command "Remove-Item -Recurse -Force core/web/dist -ErrorAction SilentlyContinue; Copy-Item -Recurse frontend/dist core/web/dist"

echo [release] building core...
if not exist bin mkdir bin
cd core
rem -H windowsgui: core 以 GUI 子系统编译，桌面壳（无控制台）spawn 它时不会弹出空终端窗口
go build -ldflags "-H windowsgui" -o ..\bin\ezharness-core.exe .
if errorlevel 1 exit /b 1
cd ..

echo [release] packaging desktop (electron-builder)...
cd desktop
call npx electron-builder --win --config.directories.output=../release/v%VERSION%
if errorlevel 1 exit /b 1
cd ..

echo.
echo release done:
echo   release\v%VERSION%\ezharness-v%VERSION%-setup.exe
echo   release\v%VERSION%\ezharness-v%VERSION%.exe
