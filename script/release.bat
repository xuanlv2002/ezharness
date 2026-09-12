@echo off
rem release: versioned full build (version source: script/version.yaml)
rem chain: frontend build -> copy dist to core/dist -> go build core -> electron-builder
rem output:
rem   release\v<version>\ezharness-v<version>-setup.exe   NSIS installer
rem   release\v<version>\ezharness-v<version>.exe         portable exe
setlocal EnableExtensions
cd /d "%~dp0.."

if not exist script\version.yaml goto nover
for /f "tokens=2 delims=: " %%v in ('findstr /b /c:"version:" script\version.yaml') do set VERSION=%%v
if not defined VERSION goto nover

rem sync version: frontend gets v-prefixed string (footer: ezharness-v<version>), desktop gets bare semver
set SRCVER=%VERSION%
powershell -NoProfile -Command "$v = $env:SRCVER; $f = 'frontend\package.json'; $j = Get-Content $f -Raw -Encoding UTF8; $j = $j -replace '\"version\": \"[^\"]*\"', ('\"version\": \"v' + $v + '\"'); [IO.File]::WriteAllText($f, $j); $d = 'desktop\package.json'; $j = Get-Content $d -Raw -Encoding UTF8; $j = $j -replace '\"version\": \"[^\"]*\"', ('\"version\": \"' + $v + '\"'); [IO.File]::WriteAllText($d, $j)"
if errorlevel 1 goto err

if not exist frontend\node_modules (
  echo [release] frontend dependencies missing, running npm install...
  cd frontend
  call npm install
  if errorlevel 1 goto err
  cd ..
)

echo [release] building frontend...
cd frontend
call npm run build
if errorlevel 1 goto err
cd ..

powershell -NoProfile -Command "Remove-Item -Recurse -Force core/dist -ErrorAction SilentlyContinue; Copy-Item -Recurse frontend/dist core/dist"

echo [release] building core...
cd core
go build -o ..\ezharness-core.exe .
if errorlevel 1 goto err
cd ..

if not exist desktop\node_modules (
  echo [release] desktop dependencies missing, running npm install...
  cd desktop
  call npm install
  if errorlevel 1 goto err
  cd ..
)

echo [release] packaging desktop (electron-builder)...
cd desktop
call npx electron-builder --win
if errorlevel 1 goto err
cd ..

if not exist "release\v%VERSION%" mkdir "release\v%VERSION%"
copy /Y "desktop\release\ezharness-v%VERSION%-setup.exe" "release\v%VERSION%\" >nul
if errorlevel 1 goto err
copy /Y "desktop\release\ezharness-v%VERSION%.exe" "release\v%VERSION%\" >nul
if errorlevel 1 goto err

echo.
echo release done:
echo   release\v%VERSION%\ezharness-v%VERSION%-setup.exe
echo   release\v%VERSION%\ezharness-v%VERSION%.exe
goto :eof

:nover
echo release.bat failed: script\version.yaml missing or has no version
exit /b 1

:err
echo release.bat failed
exit /b 1
