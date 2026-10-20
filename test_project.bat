@echo off
setlocal enabledelayedexpansion

:: Force local Go toolchain to avoid version download hangs
set GOTOOLCHAIN=local

:: Target IP Configuration (Configured to 192.168.0.113)
if "%TARGET_IP%"=="" set TARGET_IP=192.168.0.113
set GATEWAY_URL=http://%TARGET_IP%:8080
set REDIS_ADDRS=%TARGET_IP%:6379
set MONGO_URI=mongodb://%TARGET_IP%:27017
set KAFKA_BROKERS=%TARGET_IP%:9092

:: Set working directory to repository root
cd /d "%~dp0"

:: If an argument was passed directly from CLI, jump to that target
if not "%~1"=="" goto handle_args

:menu
cls
echo ================================================================================
echo          ^|  REAL-TIME RIDE DISPATCH ENGINE - TEST SUITE (WINDOWS) ^|
echo          ^|  Target IP: %TARGET_IP% ^| Gateway: %GATEWAY_URL%
echo ================================================================================
echo.
echo   [1] Run Unit Tests               (go test -race -count=1 ./internal/...)
echo   [2] Run Concurrency Race Tests   (go test -race -count=20 ./test/concurrency/...)
echo   [3] Run Integration Tests        (go test -v -race ./test/integration/...)
echo   [4] Check Code Formatting        (gofmt -l .)
echo   [5] Run Static Analysis (Vet)    (go vet ./...)
echo   [6] Run ALL Tests (Complete)     (Format + Vet + Unit + Concurrency)
echo.
echo   -- DATA ^& INFRASTRUCTURE SCRIPTS (TARGET: %TARGET_IP%) --
echo   [7] Seed Drivers to Redis        (go run scripts/seed_drivers.go -count=5000)
echo   [8] Create MongoDB Indexes       (go run scripts/create_indexes.go)
echo.
echo   -- BENCHMARKS ^& CHAOS TESTS (TARGET: %GATEWAY_URL%) --
echo   [9] Run Synthetic Load Gen       (go run ./cmd/loadgen -gateway=%GATEWAY_URL%)
echo   [10] Run Python Chaos Test       (python scripts/chaos_test.py)
echo   [11] Run Python Deep Benchmark   (python scripts/deep_infrastructure_benchmark.py)
echo.
echo   [0] Exit
echo.
echo ================================================================================
set /p choice="Enter your choice [0-11]: "

if "%choice%"=="1" goto run_unit
if "%choice%"=="2" goto run_concurrency
if "%choice%"=="3" goto run_integration
if "%choice%"=="4" goto run_fmt
if "%choice%"=="5" goto run_vet
if "%choice%"=="6" goto run_all
if "%choice%"=="7" goto run_seed
if "%choice%"=="8" goto run_indexes
if "%choice%"=="9" goto run_loadgen
if "%choice%"=="10" goto run_chaos
if "%choice%"=="11" goto run_deep_bench
if "%choice%"=="0" goto end

echo.
echo [!] Invalid option. Please try again.
timeout /t 2 >nul
goto menu

:handle_args
set "ARG=%~1"
if /i "%ARG%"=="unit" goto run_unit
if /i "%ARG%"=="concurrency" goto run_concurrency
if /i "%ARG%"=="race" goto run_concurrency
if /i "%ARG%"=="integration" goto run_integration
if /i "%ARG%"=="fmt" goto run_fmt
if /i "%ARG%"=="vet" goto run_vet
if /i "%ARG%"=="all" goto run_all
if /i "%ARG%"=="seed" goto run_seed
if /i "%ARG%"=="indexes" goto run_indexes
if /i "%ARG%"=="load" goto run_loadgen
if /i "%ARG%"=="chaos" goto run_chaos
if /i "%ARG%"=="bench" goto run_deep_bench
echo [!] Unknown argument '%ARG%'.
echo Valid options: unit, race, integration, fmt, vet, all, seed, indexes, load, chaos, bench
exit /b 1

:: -----------------------------------------------------------------------------
:run_unit
echo.
echo ================================================================================
echo  RUNNING UNIT TESTS (WITH RACE DETECTOR)
echo  Command: go test -race -count=1 ./internal/...
echo ================================================================================
go test -race -count=1 ./internal/...
if %ERRORLEVEL% NEQ 0 (
    echo.
    echo [FAIL] Unit tests failed!
) else (
    echo.
    echo [PASS] All unit tests passed cleanly!
)
goto finish

:: -----------------------------------------------------------------------------
:run_concurrency
echo.
echo ================================================================================
echo  RUNNING CONCURRENCY RACE TESTS (20 ITERATIONS - ZERO DOUBLE BOOKING)
echo  Command: go test -v -race -count=20 ./test/concurrency/...
echo ================================================================================
go test -v -race -count=20 ./test/concurrency/...
if %ERRORLEVEL% NEQ 0 (
    echo.
    echo [FAIL] Concurrency race test failed!
) else (
    echo.
    echo [PASS] Concurrency test passed: Zero double-booking verified under -race!
)
goto finish

:: -----------------------------------------------------------------------------
:run_integration
echo.
echo ================================================================================
echo  RUNNING INTEGRATION TESTS (TARGET: Redis=%REDIS_ADDRS%, Mongo=%MONGO_URI%)
echo  Command: go test -v -race ./test/integration/...
echo ================================================================================
go test -v -race ./test/integration/...
goto finish

:: -----------------------------------------------------------------------------
:run_fmt
echo.
echo ================================================================================
echo  CHECKING CODE FORMATTING
echo  Command: gofmt -l .
echo ================================================================================
for /f "delims=" %%i in ('gofmt -l .') do (
    set "UNFORMATTED=%%i"
    echo   Needs formatting: %%i
)
if defined UNFORMATTED (
    echo.
    echo [FAIL] Unformatted files found. Run 'gofmt -w -s .' to fix them.
) else (
    echo.
    echo [PASS] All Go source files are formatted cleanly according to gofmt!
)
goto finish

:: -----------------------------------------------------------------------------
:run_vet
echo.
echo ================================================================================
echo  RUNNING GO STATIC ANALYSIS (VET)
echo  Command: go vet ./...
echo ================================================================================
go vet ./...
if %ERRORLEVEL% NEQ 0 (
    echo.
    echo [FAIL] go vet found issues!
) else (
    echo.
    echo [PASS] go vet passed cleanly with zero issues!
)
goto finish

:: -----------------------------------------------------------------------------
:run_all
echo.
echo ================================================================================
echo  RUNNING ALL VERIFICATION CHECKS (DEFINITION OF DONE)
echo ================================================================================

echo.
echo [1/4] Checking code formatting...
for /f "delims=" %%i in ('gofmt -l .') do (
    set "UNFORMATTED=%%i"
    echo   Needs formatting: %%i
)
if defined UNFORMATTED (
    echo [FAIL] Formatting check failed. Run 'gofmt -w -s .'
    goto finish
)
echo [PASS] Formatting is clean.

echo.
echo [2/4] Running go vet...
go vet ./...
if %ERRORLEVEL% NEQ 0 (
    echo [FAIL] go vet failed.
    goto finish
)
echo [PASS] go vet clean.

echo.
echo [3/4] Running unit tests with race detector...
go test -race -count=1 ./internal/...
if %ERRORLEVEL% NEQ 0 (
    echo [FAIL] Unit tests failed.
    goto finish
)
echo [PASS] Unit tests passed.

echo.
echo [4/4] Running concurrency race tests (20 iterations)...
go test -race -count=20 ./test/concurrency/...
if %ERRORLEVEL% NEQ 0 (
    echo [FAIL] Concurrency tests failed.
    goto finish
)
echo [PASS] Concurrency race tests passed.

echo.
echo ================================================================================
echo  ALL CHECKS PASSED SUCCESSFULLY! THE CODEBASE IS FULLY HEALTHY.
echo ================================================================================
goto finish

:: -----------------------------------------------------------------------------
:run_seed
echo.
echo ================================================================================
echo  SEEDING SIMULATED DRIVERS INTO REDIS GEO (TARGET: %REDIS_ADDRS%)
echo  Command: go run scripts/seed_drivers.go -region=downtown -count=5000
echo ================================================================================
go run scripts/seed_drivers.go -region=downtown -count=5000
goto finish

:: -----------------------------------------------------------------------------
:run_indexes
echo.
echo ================================================================================
echo  CREATING MONGODB UNIQUE INDEXES (TARGET: %MONGO_URI%)
echo  Command: go run scripts/create_indexes.go
echo ================================================================================
go run scripts/create_indexes.go
goto finish

:: -----------------------------------------------------------------------------
:run_loadgen
echo.
echo ================================================================================
echo  RUNNING SYNTHETIC LOAD GENERATOR (TARGET: %GATEWAY_URL%)
echo  Command: go run ./cmd/loadgen -gateway=%GATEWAY_URL% -qps=250 -drivers=500 -duration=15s
echo ================================================================================
go run ./cmd/loadgen -gateway=%GATEWAY_URL% -qps=250 -drivers=500 -duration=15s
goto finish

:: -----------------------------------------------------------------------------
:run_chaos
echo.
echo ================================================================================
echo  RUNNING PYTHON CHAOS TEST (TARGET: %GATEWAY_URL%)
echo  Command: python scripts/chaos_test.py
echo ================================================================================
python scripts/chaos_test.py
goto finish

:: -----------------------------------------------------------------------------
:run_deep_bench
echo.
echo ================================================================================
echo  RUNNING DEEP INFRASTRUCTURE BENCHMARK (TARGET: %GATEWAY_URL%)
echo  Command: python scripts/deep_infrastructure_benchmark.py --gateway=%GATEWAY_URL% --qps=250 --drivers=500 --duration=10s
echo ================================================================================
python scripts/deep_infrastructure_benchmark.py --gateway=%GATEWAY_URL% --qps=250 --drivers=500 --duration=10s
goto finish

:: -----------------------------------------------------------------------------
:finish
echo.
if "%~1"=="" (
    pause
    goto menu
)

:end
exit /b 0
