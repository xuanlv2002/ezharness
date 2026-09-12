@echo off
rem dev: development build (version source: script/version.yaml)
rem chain: frontend build -> copy dist to core/dist -> go build core -> electron-builder
rem output:
rem   release\dev\ezharness-dev.exe   portable dev snapshot (frontend footer: ezharness-dev)
setlocal EnableExtensions
cd /d "%~dp0.."

if not exist script\version.yaml goto nover
for /f "tokens=2 delims=: " %%v in ('findstr /b /c:"version:" script\version.yaml') do set VERSION=%%v
if not defined VERSION goto nover

rem frontend version = dev (footer shows ezharness-dev); desktop keeps semver from version.yaml
set SRCVER=%VERSION%
powershell -NoProfile -Command "$v = $env:SRCVER; $f = 'frontend\package.json'; $j = Get-Content $f -Raw -Encoding UTF8; $j = $j -replace '\"version\": \"[^\"]*\"', '\"version\": \"dev\"'; [IO.File]::WriteAllText($f, $j); $d = 'desktop\package.json'; $j = Get-Content $d -Raw -Encoding UTF8; $j = $j -replace '\"version\": \"[^\"]*\"', ('\"version\": \"' + $v + '\"'); [IO.File]::WriteAllText($d, $j)"
if errorlevel 1 goto err

if not exist frontend\node_modules (
  echo [dev] frontend dependencies missing, running npm install...
  cd frontend
  call npm install
  if errorlevel 1 goto err
  cd ..
)

echo [dev] building frontend...
cd frontend
call npm run build
if errorlevel 1 goto err
cd ..

powershell -NoProfile -Command "Remove-Item -Recurse -Force core/dist -ErrorAction SilentlyContinue; Copy-Item -Recurse frontend/dist core/dist"

echo [dev] building core...
cd core
go build -o ..\ezharness-core.exe .
if errorlevel 1 goto err
cd ..

if not exist desktop\node_modules (
  echo [dev] desktop dependencies missing, running npm install...
  cd desktop
  call npm install
  if errorlevel 1 goto err
  cd ..
)

echo [dev] packaging desktop (electron-builder portable)...
cd desktop
call npx electron-builder --win portable --config.portable.artifactName=ezharness-dev.exe
if errorlevel 1 goto err
cd ..

if not exist "release\dev" mkdir "release\dev"
copy /Y "desktop\release\ezharness-dev.exe" "release\dev\" >nul
if errorlevel 1 goto err

echo.
echo dev build done:
echo   release\dev\ezharness-dev.exe
goto :eof

:nover
echo dev.bat failed: script\version.yaml missing or has no version
exit /b 1

:err
echo dev.bat failed
exit /b 1
