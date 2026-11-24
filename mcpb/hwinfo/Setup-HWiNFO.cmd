@echo off
rem Sets up the copy of HWiNFO that came with the hwinfo-mcp bundle.
rem
rem It is for you to run: double-click it. It is meant for a PC that has no
rem HWiNFO: where HWiNFO is installed in its usual place, it says so and does
rem nothing. Otherwise it does three things and no more:
rem
rem   1. It unpacks HWiNFO's portable package, which sits next to this file,
rem      into the folder hwinfo-mcp\HWiNFO of your local application data,
rem      unless HWiNFO is already there.
rem   2. It puts this project's settings file next to it, which starts HWiNFO
rem      in sensors-only mode with Shared Memory Support on, unless a settings
rem      file is already there.
rem   3. It starts HWiNFO, unless a HWiNFO is already running. Windows then
rem      asks for administrator rights, which HWiNFO needs to reach the
rem      hardware.
rem
rem It never closes, restarts or reconfigures a HWiNFO that is running, and it
rem does not make HWiNFO start with Windows. The free edition of HWiNFO
rem switches Shared Memory Support off after 12 hours; switching it on again is
rem done by hand, in HWiNFO's settings.
rem
rem   Setup-HWiNFO.cmd /nostart     unpack and add the settings, start nothing
rem   Setup-HWiNFO.cmd /portable    set this copy up although HWiNFO is installed
rem
rem Set HWINFO_MCP_HWINFO_DIR to use another folder.

setlocal EnableExtensions DisableDelayedExpansion
title HWiNFO setup for hwinfo-mcp

set "SRC=%~dp0"
set "DEST=%LOCALAPPDATA%\hwinfo-mcp\HWiNFO"
if defined HWINFO_MCP_HWINFO_DIR set "DEST=%HWINFO_MCP_HWINFO_DIR%"
set "START=1"
set "PORTABLE="
set "STATUS=0"

:arguments
if "%~1"=="" goto arguments_read
if /I "%~1"=="/nostart" set "START="
if /I "%~1"=="/portable" set "PORTABLE=1"
shift /1
goto arguments
:arguments_read

set "EXE=HWiNFO64.exe"
if /I "%PROCESSOR_ARCHITECTURE%"=="ARM64" set "EXE=HWiNFO_ARM64.exe"
if /I "%PROCESSOR_ARCHITEW6432%"=="ARM64" set "EXE=HWiNFO_ARM64.exe"

echo HWiNFO setup for hwinfo-mcp
echo.

rem A PC that has HWiNFO installed needs no second copy next to it, and two
rem copies with settings of their own are easy to mix up.
if defined PORTABLE goto unpack
set "INSTALLED="
if exist "%ProgramFiles%\HWiNFO64\%EXE%" set "INSTALLED=%ProgramFiles%\HWiNFO64"
if defined ProgramW6432 if exist "%ProgramW6432%\HWiNFO64\%EXE%" set "INSTALLED=%ProgramW6432%\HWiNFO64"
if defined INSTALLED goto installed

:unpack
if exist "%DEST%\%EXE%" goto unpacked

set "ZIP="
for %%F in ("%SRC%hwi_*.zip") do set "ZIP=%%~fF"
if not defined ZIP goto no_package
rem Windows' own tar reads zip archives. Another tar found first on the PATH,
rem such as the one that comes with Git, may not.
set "TAR=%SystemRoot%\System32\tar.exe"
if not exist "%TAR%" goto no_tar
if not exist "%DEST%\" mkdir "%DEST%"
if not exist "%DEST%\" goto no_folder
"%TAR%" -xf "%ZIP%" -C "%DEST%"
if errorlevel 1 goto unpack_failed
if not exist "%DEST%\%EXE%" goto unpack_failed
echo Unpacked HWiNFO into:
echo   "%DEST%"
goto settings

:unpacked
echo HWiNFO is already in this folder and was left as it is:
echo   "%DEST%"

:settings
if /I not "%EXE%"=="HWiNFO64.exe" goto settings_by_hand
if exist "%DEST%\HWiNFO64.INI" goto settings_kept
copy /Y "%SRC%HWiNFO64.INI" "%DEST%\HWiNFO64.INI" >nul
if errorlevel 1 goto settings_failed
echo Added the settings file: sensors-only mode, Shared Memory Support on.
goto start

:settings_kept
echo The settings file that was already there was left as it is.
goto start

:settings_by_hand
echo No settings file was added: this project does not know its name on
echo Windows on Arm. After HWiNFO starts, enable "Shared Memory Support" in
echo its settings and restart it.

:start
if not defined START goto finish
set "RUNNING="
for %%P in (HWiNFO64.exe HWiNFO32.exe HWiNFO_ARM64.exe) do call :is_running %%P
if defined RUNNING goto running
rem start leaves the error level as it was when it succeeds, so clear it first.
call :ok
start "" "%DEST%\%EXE%"
if errorlevel 1 goto start_failed
echo.
echo HWiNFO is starting. Windows asks for administrator rights first; the
echo publisher it names should be REALiX.
goto notes

:running
echo.
echo %RUNNING% is already running, so nothing was started. If its readings
echo are not being published, enable "Shared Memory Support" in HWiNFO's
echo settings yourself.

:notes
echo.
echo HWiNFO is freeware for non-commercial use, under its own licence:
echo   https://www.hwinfo.com/licenses/
echo Its free edition switches Shared Memory Support off after 12 hours.
echo Switching it on again is done by hand, in HWiNFO's settings.
goto finish

:installed
echo HWiNFO is already installed on this PC, in:
echo   "%INSTALLED%"
echo so the copy that came with hwinfo-mcp was not set up. Start the HWiNFO you
echo have and enable "Shared Memory Support" in its settings.
echo.
echo To set this copy up next to it all the same, run this file with /portable.
goto finish

:no_package
echo The HWiNFO package was not found next to this file, in:
echo   "%SRC%"
set "STATUS=1"
goto finish

:no_tar
echo This Windows has no tar command to unpack the package with. Unpack
echo   "%ZIP%"
echo into a folder of your own and start %EXE% from there.
set "STATUS=1"
goto finish

:no_folder
echo This folder could not be created:
echo   "%DEST%"
set "STATUS=1"
goto finish

:unpack_failed
echo The package could not be unpacked into:
echo   "%DEST%"
set "STATUS=1"
goto finish

:settings_failed
echo The settings file could not be added, so HWiNFO was not started. Start
echo %EXE% yourself from the folder below and enable "Shared Memory Support"
echo in its settings:
echo   "%DEST%"
set "STATUS=1"
goto finish

:start_failed
echo.
echo HWiNFO was not started. If you declined the administrator prompt, run
echo this file again when you want it started.
set "STATUS=1"
goto finish

:is_running
tasklist /NH /FI "IMAGENAME eq %~1" 2>nul | find /I "%~1" >nul
if not errorlevel 1 set "RUNNING=%~1"
goto :eof

:ok
exit /b 0

:finish
echo.
if defined START pause
endlocal & exit /b %STATUS%
