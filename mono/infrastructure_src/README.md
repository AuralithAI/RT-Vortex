# Infrastructure Source Directory
# This directory contains downloaded toolchains for CI/CD environments

This folder will contain:
- `jre/` - Java Runtime Environment (downloaded by setup script)
- `gradle/` - Gradle build tool (downloaded by setup script)
- `cmake/` - CMake build system (downloaded by setup script)
- `downloads/` - Temporary download cache

## Setup

Run the setup script to download the required tools:

### Linux/macOS
```bash
./scripts/setup.sh
```

### Windows
```powershell
.\scripts\setup.ps1
```

## Environment

After setup, source the environment file:

### Linux/macOS
```bash
source infrastructure_src/env.sh
```

### Windows
```powershell
. infrastructure_src\env.ps1
```

## CI/CD

In CI/CD pipelines, the setup script handles everything:
```yaml
- name: Setup toolchains
  run: ./scripts/setup.sh --ci
```

The `--ci` flag enables:
- Non-interactive mode
- Stricter error handling
- Automatic cleanup of download archives
