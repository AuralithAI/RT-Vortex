@echo off
REM
REM Generate self-signed TLS certificates for AI PR Reviewer
REM 
REM Usage: generate-certs.bat [output-dir]
REM
REM Requires: OpenSSL (install via chocolatey: choco install openssl)
REM

setlocal enabledelayedexpansion

set SCRIPT_DIR=%~dp0
set OUTPUT_DIR=%~1
if "%OUTPUT_DIR%"=="" set OUTPUT_DIR=%SCRIPT_DIR%..\config\certificates

set DAYS=365
set KEY_SIZE=2048

set CA_SUBJ=/C=US/ST=Local/L=Local/O=AI-PR-Reviewer/OU=Development/CN=AIPR-CA
set SERVER_SUBJ=/C=US/ST=Local/L=Local/O=AI-PR-Reviewer/OU=Development/CN=localhost
set CLIENT_SUBJ=/C=US/ST=Local/L=Local/O=AI-PR-Reviewer/OU=Development/CN=aipr-client

echo ========================================
echo  AI PR Reviewer - Certificate Generator
echo ========================================
echo.
echo Output directory: %OUTPUT_DIR%
echo Validity: %DAYS% days
echo.

REM Check OpenSSL
where openssl >nul 2>nul
if %ERRORLEVEL% NEQ 0 (
    echo [ERROR] OpenSSL not found.
    echo         Install via: choco install openssl
    echo         Or download from: https://slproweb.com/products/Win32OpenSSL.html
    exit /b 1
)

REM Create output directory
if not exist "%OUTPUT_DIR%" mkdir "%OUTPUT_DIR%"
cd /d "%OUTPUT_DIR%"

echo [1/5] Generating CA certificate...
openssl genrsa -out ca.key %KEY_SIZE% 2>nul
openssl req -new -x509 -days %DAYS% -key ca.key -out ca.crt -subj "%CA_SUBJ%" 2>nul
echo       Created: ca.key, ca.crt

echo [2/5] Generating server certificate...
openssl genrsa -out server.key %KEY_SIZE% 2>nul
openssl req -new -key server.key -out server.csr -subj "%SERVER_SUBJ%" 2>nul

REM Create SAN config
(
echo [req]
echo distinguished_name = req_distinguished_name
echo req_extensions = v3_req
echo prompt = no
echo.
echo [req_distinguished_name]
echo CN = localhost
echo.
echo [v3_req]
echo keyUsage = keyEncipherment, dataEncipherment
echo extendedKeyUsage = serverAuth
echo subjectAltName = @alt_names
echo.
echo [alt_names]
echo DNS.1 = localhost
echo DNS.2 = *.localhost
echo IP.1 = 127.0.0.1
echo IP.2 = ::1
) > server_san.cnf

openssl x509 -req -days %DAYS% -in server.csr -CA ca.crt -CAkey ca.key -CAcreateserial -out server.crt -extfile server_san.cnf -extensions v3_req 2>nul
del /q server.csr server_san.cnf ca.srl 2>nul
echo       Created: server.key, server.crt

echo [3/5] Generating client certificate...
openssl genrsa -out client.key %KEY_SIZE% 2>nul
openssl req -new -key client.key -out client.csr -subj "%CLIENT_SUBJ%" 2>nul

(
echo [req]
echo distinguished_name = req_distinguished_name
echo req_extensions = v3_req
echo prompt = no
echo.
echo [req_distinguished_name]
echo CN = aipr-client
echo.
echo [v3_req]
echo keyUsage = digitalSignature
echo extendedKeyUsage = clientAuth
) > client_san.cnf

openssl x509 -req -days %DAYS% -in client.csr -CA ca.crt -CAkey ca.key -CAcreateserial -out client.crt -extfile client_san.cnf -extensions v3_req 2>nul
del /q client.csr client_san.cnf ca.srl 2>nul
echo       Created: client.key, client.crt

echo [4/5] Files created...
echo       (Windows does not support Unix file permissions)

echo [5/5] Verifying certificates...
openssl verify -CAfile ca.crt server.crt >nul 2>&1 && echo       Server cert: OK
openssl verify -CAfile ca.crt client.crt >nul 2>&1 && echo       Client cert: OK

echo.
echo ========================================
echo  Certificates Generated Successfully!
echo ========================================
echo.
echo Files created in: %OUTPUT_DIR%
echo.
dir /b "%OUTPUT_DIR%\*.crt" "%OUTPUT_DIR%\*.key" 2>nul
echo.
echo WARNING: These are self-signed certificates for DEVELOPMENT ONLY.
echo          For production, use certificates from a trusted CA.
echo.

endlocal
